package libveritas

// #cgo linux,amd64 LDFLAGS: ${SRCDIR}/native/linux-amd64/liblibveritas_uniffi.a -lm -ldl -lpthread
// #cgo darwin,arm64 LDFLAGS: ${SRCDIR}/native/darwin-arm64/liblibveritas_uniffi.a -framework Security -framework CoreFoundation -lm
// #cgo windows,amd64 LDFLAGS: ${SRCDIR}/native/windows-amd64/libveritas_uniffi.lib -lws2_32 -lbcrypt -luserenv -lntdll
// #include <libveritas_uniffi.h>
import "C"

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// This is needed, because as of go 1.24
// type RustBuffer C.RustBuffer cannot have methods,
// RustBuffer is treated as non-local type
type GoRustBuffer struct {
	inner C.RustBuffer
}

type RustBufferI interface {
	AsReader() *bytes.Reader
	Free()
	ToGoBytes() []byte
	Data() unsafe.Pointer
	Len() uint64
	Capacity() uint64
}

// C.RustBuffer fields exposed as an interface so they can be accessed in different Go packages.
// See https://github.com/golang/go/issues/13467
type ExternalCRustBuffer interface {
	Data() unsafe.Pointer
	Len() uint64
	Capacity() uint64
}

func RustBufferFromC(b C.RustBuffer) ExternalCRustBuffer {
	return GoRustBuffer{
		inner: b,
	}
}

func CFromRustBuffer(b ExternalCRustBuffer) C.RustBuffer {
	return C.RustBuffer{
		capacity: C.uint64_t(b.Capacity()),
		len:      C.uint64_t(b.Len()),
		data:     (*C.uchar)(b.Data()),
	}
}

func RustBufferFromExternal(b ExternalCRustBuffer) GoRustBuffer {
	return GoRustBuffer{
		inner: C.RustBuffer{
			capacity: C.uint64_t(b.Capacity()),
			len:      C.uint64_t(b.Len()),
			data:     (*C.uchar)(b.Data()),
		},
	}
}

func (cb GoRustBuffer) Capacity() uint64 {
	return uint64(cb.inner.capacity)
}

func (cb GoRustBuffer) Len() uint64 {
	return uint64(cb.inner.len)
}

func (cb GoRustBuffer) Data() unsafe.Pointer {
	return unsafe.Pointer(cb.inner.data)
}

func (cb GoRustBuffer) AsReader() *bytes.Reader {
	b := unsafe.Slice((*byte)(cb.inner.data), C.uint64_t(cb.inner.len))
	return bytes.NewReader(b)
}

func (cb GoRustBuffer) Free() {
	rustCall(func(status *C.RustCallStatus) bool {
		C.ffi_libveritas_uniffi_rustbuffer_free(cb.inner, status)
		return false
	})
}

func (cb GoRustBuffer) ToGoBytes() []byte {
	return C.GoBytes(unsafe.Pointer(cb.inner.data), C.int(cb.inner.len))
}

func stringToRustBuffer(str string) C.RustBuffer {
	return bytesToRustBuffer([]byte(str))
}

func bytesToRustBuffer(b []byte) C.RustBuffer {
	if len(b) == 0 {
		return C.RustBuffer{}
	}
	// We can pass the pointer along here, as it is pinned
	// for the duration of this call
	foreign := C.ForeignBytes{
		len:  C.int(len(b)),
		data: (*C.uchar)(unsafe.Pointer(&b[0])),
	}

	return rustCall(func(status *C.RustCallStatus) C.RustBuffer {
		return C.ffi_libveritas_uniffi_rustbuffer_from_bytes(foreign, status)
	})
}

type BufLifter[GoType any] interface {
	Lift(value RustBufferI) GoType
}

type BufLowerer[GoType any] interface {
	Lower(value GoType) C.RustBuffer
}

type BufReader[GoType any] interface {
	Read(reader io.Reader) GoType
}

type BufWriter[GoType any] interface {
	Write(writer io.Writer, value GoType)
}

func LowerIntoRustBuffer[GoType any](bufWriter BufWriter[GoType], value GoType) C.RustBuffer {
	// This might be not the most efficient way but it does not require knowing allocation size
	// beforehand
	var buffer bytes.Buffer
	bufWriter.Write(&buffer, value)

	bytes, err := io.ReadAll(&buffer)
	if err != nil {
		panic(fmt.Errorf("reading written data: %w", err))
	}
	return bytesToRustBuffer(bytes)
}

func LiftFromRustBuffer[GoType any](bufReader BufReader[GoType], rbuf RustBufferI) GoType {
	defer rbuf.Free()
	reader := rbuf.AsReader()
	item := bufReader.Read(reader)
	if reader.Len() > 0 {
		// TODO: Remove this
		leftover, _ := io.ReadAll(reader)
		panic(fmt.Errorf("Junk remaining in buffer after lifting: %s", string(leftover)))
	}
	return item
}

func rustCallWithError[E any, U any](converter BufReader[*E], callback func(*C.RustCallStatus) U) (U, *E) {
	var status C.RustCallStatus
	returnValue := callback(&status)
	err := checkCallStatus(converter, status)
	return returnValue, err
}

func checkCallStatus[E any](converter BufReader[*E], status C.RustCallStatus) *E {
	switch status.code {
	case 0:
		return nil
	case 1:
		return LiftFromRustBuffer(converter, GoRustBuffer{inner: status.errorBuf})
	case 2:
		// when the rust code sees a panic, it tries to construct a rustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{inner: status.errorBuf})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		panic(fmt.Errorf("unknown status code: %d", status.code))
	}
}

func checkCallStatusUnknown(status C.RustCallStatus) error {
	switch status.code {
	case 0:
		return nil
	case 1:
		panic(fmt.Errorf("function not returning an error returned an error"))
	case 2:
		// when the rust code sees a panic, it tries to construct a C.RustBuffer
		// with the message.  but if that code panics, then it just sends back
		// an empty buffer.
		if status.errorBuf.len > 0 {
			panic(fmt.Errorf("%s", FfiConverterStringINSTANCE.Lift(GoRustBuffer{
				inner: status.errorBuf,
			})))
		} else {
			panic(fmt.Errorf("Rust panicked while handling Rust panic"))
		}
	default:
		return fmt.Errorf("unknown status code: %d", status.code)
	}
}

func rustCall[U any](callback func(*C.RustCallStatus) U) U {
	returnValue, err := rustCallWithError[error](nil, callback)
	if err != nil {
		panic(err)
	}
	return returnValue
}

type NativeError interface {
	AsError() error
}

func writeInt8(writer io.Writer, value int8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint8(writer io.Writer, value uint8) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt16(writer io.Writer, value int16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint16(writer io.Writer, value uint16) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt32(writer io.Writer, value int32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint32(writer io.Writer, value uint32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeInt64(writer io.Writer, value int64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeUint64(writer io.Writer, value uint64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat32(writer io.Writer, value float32) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func writeFloat64(writer io.Writer, value float64) {
	if err := binary.Write(writer, binary.BigEndian, value); err != nil {
		panic(err)
	}
}

func readInt8(reader io.Reader) int8 {
	var result int8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint8(reader io.Reader) uint8 {
	var result uint8
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt16(reader io.Reader) int16 {
	var result int16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint16(reader io.Reader) uint16 {
	var result uint16
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt32(reader io.Reader) int32 {
	var result int32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint32(reader io.Reader) uint32 {
	var result uint32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readInt64(reader io.Reader) int64 {
	var result int64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readUint64(reader io.Reader) uint64 {
	var result uint64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat32(reader io.Reader) float32 {
	var result float32
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func readFloat64(reader io.Reader) float64 {
	var result float64
	if err := binary.Read(reader, binary.BigEndian, &result); err != nil {
		panic(err)
	}
	return result
}

func init() {

	uniffiCheckChecksums()
}

func uniffiCheckChecksums() {
	// Get the bindings contract version from our ComponentInterface
	bindingsContractVersion := 29
	// Get the scaffolding contract version by calling the into the dylib
	scaffoldingContractVersion := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.ffi_libveritas_uniffi_uniffi_contract_version()
	})
	if bindingsContractVersion != int(scaffoldingContractVersion) {
		// If this happens try cleaning and rebuilding your project
		panic("libveritas_uniffi: UniFFI contract version mismatch")
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_create_certificate_chain()
		})
		if checksum != 19194 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_create_certificate_chain: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_create_offchain_records()
		})
		if checksum != 61178 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_create_offchain_records: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_decode_certificate()
		})
		if checksum != 45631 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_decode_certificate: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_decode_zone()
		})
		if checksum != 2404 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_decode_zone: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_hash_signable_message()
		})
		if checksum != 19660 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_hash_signable_message: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_verify_default()
		})
		if checksum != 23067 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_verify_default: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_verify_dev_mode()
		})
		if checksum != 61727 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_verify_dev_mode: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_verify_enable_snark()
		})
		if checksum != 2995 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_verify_enable_snark: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_verify_schnorr()
		})
		if checksum != 32215 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_verify_schnorr: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_verify_spaces_message()
		})
		if checksum != 40969 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_verify_spaces_message: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_zone_is_better_than()
		})
		if checksum != 20955 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_zone_is_better_than: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_zone_to_bytes()
		})
		if checksum != 56999 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_zone_to_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_func_zone_to_json()
		})
		if checksum != 31167 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_zone_to_json: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_anchors_compute_anchor_set_hash()
		})
		if checksum != 17244 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_anchors_compute_anchor_set_hash: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_lookup_advance()
		})
		if checksum != 31693 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_lookup_advance: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_lookup_expand_zones()
		})
		if checksum != 36867 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_lookup_expand_zones: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_lookup_start()
		})
		if checksum != 23573 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_lookup_start: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_message_to_bytes()
		})
		if checksum != 48939 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_message_to_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_message_update()
		})
		if checksum != 62656 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_message_update: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_cert()
		})
		if checksum != 30962 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_cert: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_chain()
		})
		if checksum != 45223 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_chain: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_handle()
		})
		if checksum != 36693 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_handle: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_records()
		})
		if checksum != 15630 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_records: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_update()
		})
		if checksum != 42583 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_add_update: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_build()
		})
		if checksum != 53738 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_build: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_messagebuilder_chain_proof_request()
		})
		if checksum != 17065 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_messagebuilder_chain_proof_request: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_querycontext_add_request()
		})
		if checksum != 60666 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_querycontext_add_request: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_querycontext_add_zone()
		})
		if checksum != 53280 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_querycontext_add_zone: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_recordset_is_empty()
		})
		if checksum != 27126 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_recordset_is_empty: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_recordset_signing_id()
		})
		if checksum != 30761 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_recordset_signing_id: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_recordset_to_bytes()
		})
		if checksum != 40275 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_recordset_to_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_recordset_unpack()
		})
		if checksum != 33106 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_recordset_unpack: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificate()
		})
		if checksum != 39006 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificate: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificates()
		})
		if checksum != 2792 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificates: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_message()
		})
		if checksum != 38387 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_message: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_message_bytes()
		})
		if checksum != 5718 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_message_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_zones()
		})
		if checksum != 535 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_zones: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_compute_anchor_set_hash()
		})
		if checksum != 3198 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_compute_anchor_set_hash: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_is_finalized()
		})
		if checksum != 15029 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_is_finalized: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_newest_anchor()
		})
		if checksum != 205 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_newest_anchor: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_oldest_anchor()
		})
		if checksum != 57268 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_oldest_anchor: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_sovereignty_for()
		})
		if checksum != 5317 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_sovereignty_for: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_verify()
		})
		if checksum != 24000 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_verify: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_verify_with_options()
		})
		if checksum != 12524 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_verify_with_options: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_anchors_from_json()
		})
		if checksum != 36150 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_anchors_from_json: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_lookup_new()
		})
		if checksum != 37506 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_lookup_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_message_new()
		})
		if checksum != 52348 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_message_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_messagebuilder_new()
		})
		if checksum != 51295 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_messagebuilder_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_querycontext_new()
		})
		if checksum != 17395 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_querycontext_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_recordset_new()
		})
		if checksum != 33356 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_recordset_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_recordset_pack()
		})
		if checksum != 59235 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_recordset_pack: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_veritas_new()
		})
		if checksum != 7569 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_veritas_new: UniFFI API checksum mismatch")
		}
	}
}

type FfiConverterUint8 struct{}

var FfiConverterUint8INSTANCE = FfiConverterUint8{}

func (FfiConverterUint8) Lower(value uint8) C.uint8_t {
	return C.uint8_t(value)
}

func (FfiConverterUint8) Write(writer io.Writer, value uint8) {
	writeUint8(writer, value)
}

func (FfiConverterUint8) Lift(value C.uint8_t) uint8 {
	return uint8(value)
}

func (FfiConverterUint8) Read(reader io.Reader) uint8 {
	return readUint8(reader)
}

type FfiDestroyerUint8 struct{}

func (FfiDestroyerUint8) Destroy(_ uint8) {}

type FfiConverterUint32 struct{}

var FfiConverterUint32INSTANCE = FfiConverterUint32{}

func (FfiConverterUint32) Lower(value uint32) C.uint32_t {
	return C.uint32_t(value)
}

func (FfiConverterUint32) Write(writer io.Writer, value uint32) {
	writeUint32(writer, value)
}

func (FfiConverterUint32) Lift(value C.uint32_t) uint32 {
	return uint32(value)
}

func (FfiConverterUint32) Read(reader io.Reader) uint32 {
	return readUint32(reader)
}

type FfiDestroyerUint32 struct{}

func (FfiDestroyerUint32) Destroy(_ uint32) {}

type FfiConverterUint64 struct{}

var FfiConverterUint64INSTANCE = FfiConverterUint64{}

func (FfiConverterUint64) Lower(value uint64) C.uint64_t {
	return C.uint64_t(value)
}

func (FfiConverterUint64) Write(writer io.Writer, value uint64) {
	writeUint64(writer, value)
}

func (FfiConverterUint64) Lift(value C.uint64_t) uint64 {
	return uint64(value)
}

func (FfiConverterUint64) Read(reader io.Reader) uint64 {
	return readUint64(reader)
}

type FfiDestroyerUint64 struct{}

func (FfiDestroyerUint64) Destroy(_ uint64) {}

type FfiConverterBool struct{}

var FfiConverterBoolINSTANCE = FfiConverterBool{}

func (FfiConverterBool) Lower(value bool) C.int8_t {
	if value {
		return C.int8_t(1)
	}
	return C.int8_t(0)
}

func (FfiConverterBool) Write(writer io.Writer, value bool) {
	if value {
		writeInt8(writer, 1)
	} else {
		writeInt8(writer, 0)
	}
}

func (FfiConverterBool) Lift(value C.int8_t) bool {
	return value != 0
}

func (FfiConverterBool) Read(reader io.Reader) bool {
	return readInt8(reader) != 0
}

type FfiDestroyerBool struct{}

func (FfiDestroyerBool) Destroy(_ bool) {}

type FfiConverterString struct{}

var FfiConverterStringINSTANCE = FfiConverterString{}

func (FfiConverterString) Lift(rb RustBufferI) string {
	defer rb.Free()
	reader := rb.AsReader()
	b, err := io.ReadAll(reader)
	if err != nil {
		panic(fmt.Errorf("reading reader: %w", err))
	}
	return string(b)
}

func (FfiConverterString) Read(reader io.Reader) string {
	length := readInt32(reader)
	buffer := make([]byte, length)
	read_length, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading string, expected %d, read %d", length, read_length))
	}
	return string(buffer)
}

func (FfiConverterString) Lower(value string) C.RustBuffer {
	return stringToRustBuffer(value)
}

func (c FfiConverterString) LowerExternal(value string) ExternalCRustBuffer {
	return RustBufferFromC(stringToRustBuffer(value))
}

func (FfiConverterString) Write(writer io.Writer, value string) {
	if len(value) > math.MaxInt32 {
		panic("String is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	write_length, err := io.WriteString(writer, value)
	if err != nil {
		panic(err)
	}
	if write_length != len(value) {
		panic(fmt.Errorf("bad write length when writing string, expected %d, written %d", len(value), write_length))
	}
}

type FfiDestroyerString struct{}

func (FfiDestroyerString) Destroy(_ string) {}

type FfiConverterBytes struct{}

var FfiConverterBytesINSTANCE = FfiConverterBytes{}

func (c FfiConverterBytes) Lower(value []byte) C.RustBuffer {
	return LowerIntoRustBuffer[[]byte](c, value)
}

func (c FfiConverterBytes) LowerExternal(value []byte) ExternalCRustBuffer {
	return RustBufferFromC(c.Lower(value))
}

func (c FfiConverterBytes) Write(writer io.Writer, value []byte) {
	if len(value) > math.MaxInt32 {
		panic("[]byte is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	write_length, err := writer.Write(value)
	if err != nil {
		panic(err)
	}
	if write_length != len(value) {
		panic(fmt.Errorf("bad write length when writing []byte, expected %d, written %d", len(value), write_length))
	}
}

func (c FfiConverterBytes) Lift(rb RustBufferI) []byte {
	return LiftFromRustBuffer[[]byte](c, rb)
}

func (c FfiConverterBytes) Read(reader io.Reader) []byte {
	length := readInt32(reader)
	buffer := make([]byte, length)
	read_length, err := reader.Read(buffer)
	if err != nil && err != io.EOF {
		panic(err)
	}
	if read_length != int(length) {
		panic(fmt.Errorf("bad read length when reading []byte, expected %d, read %d", length, read_length))
	}
	return buffer
}

type FfiDestroyerBytes struct{}

func (FfiDestroyerBytes) Destroy(_ []byte) {}

// Below is an implementation of synchronization requirements outlined in the link.
// https://github.com/mozilla/uniffi-rs/blob/0dc031132d9493ca812c3af6e7dd60ad2ea95bf0/uniffi_bindgen/src/bindings/kotlin/templates/ObjectRuntime.kt#L31

type FfiObject struct {
	pointer       unsafe.Pointer
	callCounter   atomic.Int64
	cloneFunction func(unsafe.Pointer, *C.RustCallStatus) unsafe.Pointer
	freeFunction  func(unsafe.Pointer, *C.RustCallStatus)
	destroyed     atomic.Bool
}

func newFfiObject(
	pointer unsafe.Pointer,
	cloneFunction func(unsafe.Pointer, *C.RustCallStatus) unsafe.Pointer,
	freeFunction func(unsafe.Pointer, *C.RustCallStatus),
) FfiObject {
	return FfiObject{
		pointer:       pointer,
		cloneFunction: cloneFunction,
		freeFunction:  freeFunction,
	}
}

func (ffiObject *FfiObject) incrementPointer(debugName string) unsafe.Pointer {
	for {
		counter := ffiObject.callCounter.Load()
		if counter <= -1 {
			panic(fmt.Errorf("%v object has already been destroyed", debugName))
		}
		if counter == math.MaxInt64 {
			panic(fmt.Errorf("%v object call counter would overflow", debugName))
		}
		if ffiObject.callCounter.CompareAndSwap(counter, counter+1) {
			break
		}
	}

	return rustCall(func(status *C.RustCallStatus) unsafe.Pointer {
		return ffiObject.cloneFunction(ffiObject.pointer, status)
	})
}

func (ffiObject *FfiObject) decrementPointer() {
	if ffiObject.callCounter.Add(-1) == -1 {
		ffiObject.freeRustArcPtr()
	}
}

func (ffiObject *FfiObject) destroy() {
	if ffiObject.destroyed.CompareAndSwap(false, true) {
		if ffiObject.callCounter.Add(-1) == -1 {
			ffiObject.freeRustArcPtr()
		}
	}
}

func (ffiObject *FfiObject) freeRustArcPtr() {
	rustCall(func(status *C.RustCallStatus) int32 {
		ffiObject.freeFunction(ffiObject.pointer, status)
		return 0
	})
}

type AnchorsInterface interface {
	ComputeAnchorSetHash() []byte
}
type Anchors struct {
	ffiObject FfiObject
}

func AnchorsFromJson(json string) (*Anchors, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_anchors_from_json(FfiConverterStringINSTANCE.Lower(json), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Anchors
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterAnchorsINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Anchors) ComputeAnchorSetHash() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Anchors")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_anchors_compute_anchor_set_hash(
				_pointer, _uniffiStatus),
		}
	}))
}
func (object *Anchors) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterAnchors struct{}

var FfiConverterAnchorsINSTANCE = FfiConverterAnchors{}

func (c FfiConverterAnchors) Lift(pointer unsafe.Pointer) *Anchors {
	result := &Anchors{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_anchors(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_anchors(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Anchors).Destroy)
	return result
}

func (c FfiConverterAnchors) Read(reader io.Reader) *Anchors {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterAnchors) Lower(value *Anchors) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Anchors")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterAnchors) Write(writer io.Writer, value *Anchors) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerAnchors struct{}

func (_ FfiDestroyerAnchors) Destroy(value *Anchors) {
	value.Destroy()
}

// Batched iterative resolver for nested handle names.
type LookupInterface interface {
	// Feed zones from a resolveAll response.
	// Returns the next batch of handles to look up (empty = done).
	Advance(zones []Zone) ([]string, error)
	// Expand zone handles using the alias map accumulated during resolution.
	ExpandZones(zones []Zone) ([]Zone, error)
	// Returns the first batch of handles to look up.
	Start() []string
}

// Batched iterative resolver for nested handle names.
type Lookup struct {
	ffiObject FfiObject
}

// Create a lookup from a list of handle name strings.
func NewLookup(names []string) (*Lookup, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_lookup_new(FfiConverterSequenceStringINSTANCE.Lower(names), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Lookup
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterLookupINSTANCE.Lift(_uniffiRV), nil
	}
}

// Feed zones from a resolveAll response.
// Returns the next batch of handles to look up (empty = done).
func (_self *Lookup) Advance(zones []Zone) ([]string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Lookup")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_lookup_advance(
				_pointer, FfiConverterSequenceZoneINSTANCE.Lower(zones), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterSequenceStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Expand zone handles using the alias map accumulated during resolution.
func (_self *Lookup) ExpandZones(zones []Zone) ([]Zone, error) {
	_pointer := _self.ffiObject.incrementPointer("*Lookup")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_lookup_expand_zones(
				_pointer, FfiConverterSequenceZoneINSTANCE.Lower(zones), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []Zone
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterSequenceZoneINSTANCE.Lift(_uniffiRV), nil
	}
}

// Returns the first batch of handles to look up.
func (_self *Lookup) Start() []string {
	_pointer := _self.ffiObject.incrementPointer("*Lookup")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_lookup_start(
				_pointer, _uniffiStatus),
		}
	}))
}
func (object *Lookup) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterLookup struct{}

var FfiConverterLookupINSTANCE = FfiConverterLookup{}

func (c FfiConverterLookup) Lift(pointer unsafe.Pointer) *Lookup {
	result := &Lookup{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_lookup(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_lookup(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Lookup).Destroy)
	return result
}

func (c FfiConverterLookup) Read(reader io.Reader) *Lookup {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterLookup) Lower(value *Lookup) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Lookup")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterLookup) Write(writer io.Writer, value *Lookup) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerLookup struct{}

func (_ FfiDestroyerLookup) Destroy(value *Lookup) {
	value.Destroy()
}

type MessageInterface interface {
	// Serialize the message to borsh bytes.
	ToBytes() []byte
	// Update records on this message.
	//
	// - `name`: handle string (e.g. "alice@bitcoin", "@bitcoin", "#12-12-0")
	// - `records`: borsh-encoded OffchainRecords (optional)
	// - `delegate_records`: borsh-encoded OffchainRecords (optional)
	Update(updates []DataUpdateEntry) error
}
type Message struct {
	ffiObject FfiObject
}

// Decode a message from borsh bytes.
func NewMessage(bytes []byte) (*Message, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_message_new(FfiConverterBytesINSTANCE.Lower(bytes), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Message
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterMessageINSTANCE.Lift(_uniffiRV), nil
	}
}

// Serialize the message to borsh bytes.
func (_self *Message) ToBytes() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Message")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_message_to_bytes(
				_pointer, _uniffiStatus),
		}
	}))
}

// Update records on this message.
//
// - `name`: handle string (e.g. "alice@bitcoin", "@bitcoin", "#12-12-0")
// - `records`: borsh-encoded OffchainRecords (optional)
// - `delegate_records`: borsh-encoded OffchainRecords (optional)
func (_self *Message) Update(updates []DataUpdateEntry) error {
	_pointer := _self.ffiObject.incrementPointer("*Message")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_message_update(
			_pointer, FfiConverterSequenceDataUpdateEntryINSTANCE.Lower(updates), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}
func (object *Message) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterMessage struct{}

var FfiConverterMessageINSTANCE = FfiConverterMessage{}

func (c FfiConverterMessage) Lift(pointer unsafe.Pointer) *Message {
	result := &Message{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_message(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_message(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Message).Destroy)
	return result
}

func (c FfiConverterMessage) Read(reader io.Reader) *Message {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterMessage) Lower(value *Message) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Message")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterMessage) Write(writer io.Writer, value *Message) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerMessage struct{}

func (_ FfiDestroyerMessage) Destroy(value *Message) {
	value.Destroy()
}

// Builder for constructing messages from update requests and chain proofs.
type MessageBuilderInterface interface {
	// Add a single certificate.
	AddCert(certBytes []byte) error
	// Add all certificates from a .spacecert chain.
	AddChain(chainBytes []byte) error
	// Add a .spacecert chain with records.
	AddHandle(chainBytes []byte, recordsBytes []byte) error
	// Add records for a handle.
	AddRecords(handle string, recordsBytes []byte) error
	// Add a full data update (records + optional delegate records).
	AddUpdate(entry DataUpdateEntry) error
	// Build the message from a borsh-encoded ChainProof.
	//
	// Consumes the builder — cannot be called twice.
	Build(chainProof []byte) (*Message, error)
	// Returns the chain proof request as JSON.
	//
	// Send this to the provider/fabric to get the chain proofs needed for `build()`.
	ChainProofRequest() (string, error)
}

// Builder for constructing messages from update requests and chain proofs.
type MessageBuilder struct {
	ffiObject FfiObject
}

// Create an empty builder.
func NewMessageBuilder() *MessageBuilder {
	return FfiConverterMessageBuilderINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_messagebuilder_new(_uniffiStatus)
	}))
}

// Add a single certificate.
func (_self *MessageBuilder) AddCert(certBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_messagebuilder_add_cert(
			_pointer, FfiConverterBytesINSTANCE.Lower(certBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add all certificates from a .spacecert chain.
func (_self *MessageBuilder) AddChain(chainBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_messagebuilder_add_chain(
			_pointer, FfiConverterBytesINSTANCE.Lower(chainBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add a .spacecert chain with records.
func (_self *MessageBuilder) AddHandle(chainBytes []byte, recordsBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_messagebuilder_add_handle(
			_pointer, FfiConverterBytesINSTANCE.Lower(chainBytes), FfiConverterBytesINSTANCE.Lower(recordsBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add records for a handle.
func (_self *MessageBuilder) AddRecords(handle string, recordsBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_messagebuilder_add_records(
			_pointer, FfiConverterStringINSTANCE.Lower(handle), FfiConverterBytesINSTANCE.Lower(recordsBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add a full data update (records + optional delegate records).
func (_self *MessageBuilder) AddUpdate(entry DataUpdateEntry) error {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_messagebuilder_add_update(
			_pointer, FfiConverterDataUpdateEntryINSTANCE.Lower(entry), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Build the message from a borsh-encoded ChainProof.
//
// Consumes the builder — cannot be called twice.
func (_self *MessageBuilder) Build(chainProof []byte) (*Message, error) {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_messagebuilder_build(
			_pointer, FfiConverterBytesINSTANCE.Lower(chainProof), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Message
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterMessageINSTANCE.Lift(_uniffiRV), nil
	}
}

// Returns the chain proof request as JSON.
//
// Send this to the provider/fabric to get the chain proofs needed for `build()`.
func (_self *MessageBuilder) ChainProofRequest() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*MessageBuilder")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_messagebuilder_chain_proof_request(
				_pointer, _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}
func (object *MessageBuilder) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterMessageBuilder struct{}

var FfiConverterMessageBuilderINSTANCE = FfiConverterMessageBuilder{}

func (c FfiConverterMessageBuilder) Lift(pointer unsafe.Pointer) *MessageBuilder {
	result := &MessageBuilder{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_messagebuilder(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_messagebuilder(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*MessageBuilder).Destroy)
	return result
}

func (c FfiConverterMessageBuilder) Read(reader io.Reader) *MessageBuilder {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterMessageBuilder) Lower(value *MessageBuilder) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*MessageBuilder")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterMessageBuilder) Write(writer io.Writer, value *MessageBuilder) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerMessageBuilder struct{}

func (_ FfiDestroyerMessageBuilder) Destroy(value *MessageBuilder) {
	value.Destroy()
}

type QueryContextInterface interface {
	// Add a handle to verify (e.g. "alice@bitcoin").
	// If no requests are added, all handles in the message are verified.
	AddRequest(handle string) error
	// Add a known zone from stored bytes (from a previous verification).
	AddZone(zoneBytes []byte) error
}
type QueryContext struct {
	ffiObject FfiObject
}

func NewQueryContext() *QueryContext {
	return FfiConverterQueryContextINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_querycontext_new(_uniffiStatus)
	}))
}

// Add a handle to verify (e.g. "alice@bitcoin").
// If no requests are added, all handles in the message are verified.
func (_self *QueryContext) AddRequest(handle string) error {
	_pointer := _self.ffiObject.incrementPointer("*QueryContext")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_querycontext_add_request(
			_pointer, FfiConverterStringINSTANCE.Lower(handle), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add a known zone from stored bytes (from a previous verification).
func (_self *QueryContext) AddZone(zoneBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*QueryContext")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_querycontext_add_zone(
			_pointer, FfiConverterBytesINSTANCE.Lower(zoneBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}
func (object *QueryContext) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterQueryContext struct{}

var FfiConverterQueryContextINSTANCE = FfiConverterQueryContext{}

func (c FfiConverterQueryContext) Lift(pointer unsafe.Pointer) *QueryContext {
	result := &QueryContext{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_querycontext(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_querycontext(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*QueryContext).Destroy)
	return result
}

func (c FfiConverterQueryContext) Read(reader io.Reader) *QueryContext {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterQueryContext) Lower(value *QueryContext) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*QueryContext")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterQueryContext) Write(writer io.Writer, value *QueryContext) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerQueryContext struct{}

func (_ FfiDestroyerQueryContext) Destroy(value *QueryContext) {
	value.Destroy()
}

// SIP-7 record set — wire-format encoded records.
type RecordSetInterface interface {
	IsEmpty() bool
	// The 32-byte signing hash (Spaces signed-message prefix + SHA256).
	SigningId() []byte
	// Raw wire bytes.
	ToBytes() []byte
	// Parse all records.
	Unpack() ([]Record, error)
}

// SIP-7 record set — wire-format encoded records.
type RecordSet struct {
	ffiObject FfiObject
}

// Wrap raw wire bytes (lazy — no parsing until unpack).
func NewRecordSet(data []byte) *RecordSet {
	return FfiConverterRecordSetINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_recordset_new(FfiConverterBytesINSTANCE.Lower(data), _uniffiStatus)
	}))
}

// Pack records into wire format.
func RecordSetPack(records []Record) (*RecordSet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_recordset_pack(FfiConverterSequenceRecordINSTANCE.Lower(records), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *RecordSet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterRecordSetINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *RecordSet) IsEmpty() bool {
	_pointer := _self.ffiObject.incrementPointer("*RecordSet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBoolINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_libveritas_uniffi_fn_method_recordset_is_empty(
			_pointer, _uniffiStatus)
	}))
}

// The 32-byte signing hash (Spaces signed-message prefix + SHA256).
func (_self *RecordSet) SigningId() []byte {
	_pointer := _self.ffiObject.incrementPointer("*RecordSet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_recordset_signing_id(
				_pointer, _uniffiStatus),
		}
	}))
}

// Raw wire bytes.
func (_self *RecordSet) ToBytes() []byte {
	_pointer := _self.ffiObject.incrementPointer("*RecordSet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_recordset_to_bytes(
				_pointer, _uniffiStatus),
		}
	}))
}

// Parse all records.
func (_self *RecordSet) Unpack() ([]Record, error) {
	_pointer := _self.ffiObject.incrementPointer("*RecordSet")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_recordset_unpack(
				_pointer, _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []Record
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterSequenceRecordINSTANCE.Lift(_uniffiRV), nil
	}
}
func (object *RecordSet) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterRecordSet struct{}

var FfiConverterRecordSetINSTANCE = FfiConverterRecordSet{}

func (c FfiConverterRecordSet) Lift(pointer unsafe.Pointer) *RecordSet {
	result := &RecordSet{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_recordset(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_recordset(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*RecordSet).Destroy)
	return result
}

func (c FfiConverterRecordSet) Read(reader io.Reader) *RecordSet {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterRecordSet) Lower(value *RecordSet) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*RecordSet")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterRecordSet) Write(writer io.Writer, value *RecordSet) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerRecordSet struct{}

func (_ FfiDestroyerRecordSet) Destroy(value *RecordSet) {
	value.Destroy()
}

type VerifiedMessageInterface interface {
	Certificate(handle string) (*Certificate, error)
	Certificates() []Certificate
	// Get the verified message for rebroadcasting or updating.
	Message() *Message
	// Get the verified message as borsh bytes.
	MessageBytes() []byte
	Zones() []Zone
}
type VerifiedMessage struct {
	ffiObject FfiObject
}

func (_self *VerifiedMessage) Certificate(handle string) (*Certificate, error) {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_certificate(
				_pointer, FfiConverterStringINSTANCE.Lower(handle), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Certificate
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterOptionalCertificateINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *VerifiedMessage) Certificates() []Certificate {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceCertificateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_certificates(
				_pointer, _uniffiStatus),
		}
	}))
}

// Get the verified message for rebroadcasting or updating.
func (_self *VerifiedMessage) Message() *Message {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterMessageINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_message(
			_pointer, _uniffiStatus)
	}))
}

// Get the verified message as borsh bytes.
func (_self *VerifiedMessage) MessageBytes() []byte {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_message_bytes(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VerifiedMessage) Zones() []Zone {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceZoneINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_zones(
				_pointer, _uniffiStatus),
		}
	}))
}
func (object *VerifiedMessage) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterVerifiedMessage struct{}

var FfiConverterVerifiedMessageINSTANCE = FfiConverterVerifiedMessage{}

func (c FfiConverterVerifiedMessage) Lift(pointer unsafe.Pointer) *VerifiedMessage {
	result := &VerifiedMessage{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_verifiedmessage(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_verifiedmessage(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*VerifiedMessage).Destroy)
	return result
}

func (c FfiConverterVerifiedMessage) Read(reader io.Reader) *VerifiedMessage {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterVerifiedMessage) Lower(value *VerifiedMessage) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*VerifiedMessage")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterVerifiedMessage) Write(writer io.Writer, value *VerifiedMessage) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerVerifiedMessage struct{}

func (_ FfiDestroyerVerifiedMessage) Destroy(value *VerifiedMessage) {
	value.Destroy()
}

type VeritasInterface interface {
	ComputeAnchorSetHash() []byte
	IsFinalized(commitmentHeight uint32) bool
	NewestAnchor() uint32
	OldestAnchor() uint32
	SovereigntyFor(commitmentHeight uint32) string
	// Verify a message with default options.
	Verify(ctx *QueryContext, msg *Message) (*VerifiedMessage, error)
	// Verify a message with option flags (combine with bitwise OR).
	VerifyWithOptions(ctx *QueryContext, msg *Message, options uint32) (*VerifiedMessage, error)
}
type Veritas struct {
	ffiObject FfiObject
}

func NewVeritas(anchors *Anchors) (*Veritas, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_veritas_new(FfiConverterAnchorsINSTANCE.Lower(anchors), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Veritas
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterVeritasINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Veritas) ComputeAnchorSetHash() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritas_compute_anchor_set_hash(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Veritas) IsFinalized(commitmentHeight uint32) bool {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBoolINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_is_finalized(
			_pointer, FfiConverterUint32INSTANCE.Lower(commitmentHeight), _uniffiStatus)
	}))
}

func (_self *Veritas) NewestAnchor() uint32 {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_newest_anchor(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Veritas) OldestAnchor() uint32 {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_oldest_anchor(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Veritas) SovereigntyFor(commitmentHeight uint32) string {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritas_sovereignty_for(
				_pointer, FfiConverterUint32INSTANCE.Lower(commitmentHeight), _uniffiStatus),
		}
	}))
}

// Verify a message with default options.
func (_self *Veritas) Verify(ctx *QueryContext, msg *Message) (*VerifiedMessage, error) {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_verify(
			_pointer, FfiConverterQueryContextINSTANCE.Lower(ctx), FfiConverterMessageINSTANCE.Lower(msg), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *VerifiedMessage
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterVerifiedMessageINSTANCE.Lift(_uniffiRV), nil
	}
}

// Verify a message with option flags (combine with bitwise OR).
func (_self *Veritas) VerifyWithOptions(ctx *QueryContext, msg *Message, options uint32) (*VerifiedMessage, error) {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_verify_with_options(
			_pointer, FfiConverterQueryContextINSTANCE.Lower(ctx), FfiConverterMessageINSTANCE.Lower(msg), FfiConverterUint32INSTANCE.Lower(options), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *VerifiedMessage
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterVerifiedMessageINSTANCE.Lift(_uniffiRV), nil
	}
}
func (object *Veritas) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterVeritas struct{}

var FfiConverterVeritasINSTANCE = FfiConverterVeritas{}

func (c FfiConverterVeritas) Lift(pointer unsafe.Pointer) *Veritas {
	result := &Veritas{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_veritas(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_veritas(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Veritas).Destroy)
	return result
}

func (c FfiConverterVeritas) Read(reader io.Reader) *Veritas {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterVeritas) Lower(value *Veritas) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Veritas")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterVeritas) Write(writer io.Writer, value *Veritas) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerVeritas struct{}

func (_ FfiDestroyerVeritas) Destroy(value *Veritas) {
	value.Destroy()
}

type Certificate struct {
	Subject  string
	CertType string
	Bytes    []byte
}

func (r *Certificate) Destroy() {
	FfiDestroyerString{}.Destroy(r.Subject)
	FfiDestroyerString{}.Destroy(r.CertType)
	FfiDestroyerBytes{}.Destroy(r.Bytes)
}

type FfiConverterCertificate struct{}

var FfiConverterCertificateINSTANCE = FfiConverterCertificate{}

func (c FfiConverterCertificate) Lift(rb RustBufferI) Certificate {
	return LiftFromRustBuffer[Certificate](c, rb)
}

func (c FfiConverterCertificate) Read(reader io.Reader) Certificate {
	return Certificate{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterCertificate) Lower(value Certificate) C.RustBuffer {
	return LowerIntoRustBuffer[Certificate](c, value)
}

func (c FfiConverterCertificate) LowerExternal(value Certificate) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Certificate](c, value))
}

func (c FfiConverterCertificate) Write(writer io.Writer, value Certificate) {
	FfiConverterStringINSTANCE.Write(writer, value.Subject)
	FfiConverterStringINSTANCE.Write(writer, value.CertType)
	FfiConverterBytesINSTANCE.Write(writer, value.Bytes)
}

type FfiDestroyerCertificate struct{}

func (_ FfiDestroyerCertificate) Destroy(value Certificate) {
	value.Destroy()
}

// Data update entry for Message.update() — no cert field.
type DataUpdateEntry struct {
	Name            string
	Records         *[]byte
	DelegateRecords *[]byte
}

func (r *DataUpdateEntry) Destroy() {
	FfiDestroyerString{}.Destroy(r.Name)
	FfiDestroyerOptionalBytes{}.Destroy(r.Records)
	FfiDestroyerOptionalBytes{}.Destroy(r.DelegateRecords)
}

type FfiConverterDataUpdateEntry struct{}

var FfiConverterDataUpdateEntryINSTANCE = FfiConverterDataUpdateEntry{}

func (c FfiConverterDataUpdateEntry) Lift(rb RustBufferI) DataUpdateEntry {
	return LiftFromRustBuffer[DataUpdateEntry](c, rb)
}

func (c FfiConverterDataUpdateEntry) Read(reader io.Reader) DataUpdateEntry {
	return DataUpdateEntry{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterDataUpdateEntry) Lower(value DataUpdateEntry) C.RustBuffer {
	return LowerIntoRustBuffer[DataUpdateEntry](c, value)
}

func (c FfiConverterDataUpdateEntry) LowerExternal(value DataUpdateEntry) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[DataUpdateEntry](c, value))
}

func (c FfiConverterDataUpdateEntry) Write(writer io.Writer, value DataUpdateEntry) {
	FfiConverterStringINSTANCE.Write(writer, value.Name)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.Records)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.DelegateRecords)
}

type FfiDestroyerDataUpdateEntry struct{}

func (_ FfiDestroyerDataUpdateEntry) Destroy(value DataUpdateEntry) {
	value.Destroy()
}

type Zone struct {
	Anchor          uint32
	Sovereignty     string
	Handle          string
	Canonical       string
	Alias           *string
	ScriptPubkey    []byte
	Records         *[]byte
	FallbackRecords *[]byte
	Delegate        DelegateState
	Commitment      CommitmentState
}

func (r *Zone) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Anchor)
	FfiDestroyerString{}.Destroy(r.Sovereignty)
	FfiDestroyerString{}.Destroy(r.Handle)
	FfiDestroyerString{}.Destroy(r.Canonical)
	FfiDestroyerOptionalString{}.Destroy(r.Alias)
	FfiDestroyerBytes{}.Destroy(r.ScriptPubkey)
	FfiDestroyerOptionalBytes{}.Destroy(r.Records)
	FfiDestroyerOptionalBytes{}.Destroy(r.FallbackRecords)
	FfiDestroyerDelegateState{}.Destroy(r.Delegate)
	FfiDestroyerCommitmentState{}.Destroy(r.Commitment)
}

type FfiConverterZone struct{}

var FfiConverterZoneINSTANCE = FfiConverterZone{}

func (c FfiConverterZone) Lift(rb RustBufferI) Zone {
	return LiftFromRustBuffer[Zone](c, rb)
}

func (c FfiConverterZone) Read(reader io.Reader) Zone {
	return Zone{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalStringINSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
		FfiConverterDelegateStateINSTANCE.Read(reader),
		FfiConverterCommitmentStateINSTANCE.Read(reader),
	}
}

func (c FfiConverterZone) Lower(value Zone) C.RustBuffer {
	return LowerIntoRustBuffer[Zone](c, value)
}

func (c FfiConverterZone) LowerExternal(value Zone) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Zone](c, value))
}

func (c FfiConverterZone) Write(writer io.Writer, value Zone) {
	FfiConverterUint32INSTANCE.Write(writer, value.Anchor)
	FfiConverterStringINSTANCE.Write(writer, value.Sovereignty)
	FfiConverterStringINSTANCE.Write(writer, value.Handle)
	FfiConverterStringINSTANCE.Write(writer, value.Canonical)
	FfiConverterOptionalStringINSTANCE.Write(writer, value.Alias)
	FfiConverterBytesINSTANCE.Write(writer, value.ScriptPubkey)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.Records)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.FallbackRecords)
	FfiConverterDelegateStateINSTANCE.Write(writer, value.Delegate)
	FfiConverterCommitmentStateINSTANCE.Write(writer, value.Commitment)
}

type FfiDestroyerZone struct{}

func (_ FfiDestroyerZone) Destroy(value Zone) {
	value.Destroy()
}

type CommitmentState interface {
	Destroy()
}
type CommitmentStateExists struct {
	StateRoot   []byte
	PrevRoot    *[]byte
	RollingHash []byte
	BlockHeight uint32
	ReceiptHash *[]byte
}

func (e CommitmentStateExists) Destroy() {
	FfiDestroyerBytes{}.Destroy(e.StateRoot)
	FfiDestroyerOptionalBytes{}.Destroy(e.PrevRoot)
	FfiDestroyerBytes{}.Destroy(e.RollingHash)
	FfiDestroyerUint32{}.Destroy(e.BlockHeight)
	FfiDestroyerOptionalBytes{}.Destroy(e.ReceiptHash)
}

type CommitmentStateEmpty struct {
}

func (e CommitmentStateEmpty) Destroy() {
}

type CommitmentStateUnknown struct {
}

func (e CommitmentStateUnknown) Destroy() {
}

type FfiConverterCommitmentState struct{}

var FfiConverterCommitmentStateINSTANCE = FfiConverterCommitmentState{}

func (c FfiConverterCommitmentState) Lift(rb RustBufferI) CommitmentState {
	return LiftFromRustBuffer[CommitmentState](c, rb)
}

func (c FfiConverterCommitmentState) Lower(value CommitmentState) C.RustBuffer {
	return LowerIntoRustBuffer[CommitmentState](c, value)
}

func (c FfiConverterCommitmentState) LowerExternal(value CommitmentState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[CommitmentState](c, value))
}
func (FfiConverterCommitmentState) Read(reader io.Reader) CommitmentState {
	id := readInt32(reader)
	switch id {
	case 1:
		return CommitmentStateExists{
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
		}
	case 2:
		return CommitmentStateEmpty{}
	case 3:
		return CommitmentStateUnknown{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterCommitmentState.Read()", id))
	}
}

func (FfiConverterCommitmentState) Write(writer io.Writer, value CommitmentState) {
	switch variant_value := value.(type) {
	case CommitmentStateExists:
		writeInt32(writer, 1)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.StateRoot)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.PrevRoot)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.RollingHash)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.BlockHeight)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.ReceiptHash)
	case CommitmentStateEmpty:
		writeInt32(writer, 2)
	case CommitmentStateUnknown:
		writeInt32(writer, 3)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterCommitmentState.Write", value))
	}
}

type FfiDestroyerCommitmentState struct{}

func (_ FfiDestroyerCommitmentState) Destroy(value CommitmentState) {
	value.Destroy()
}

type DelegateState interface {
	Destroy()
}
type DelegateStateExists struct {
	ScriptPubkey    []byte
	FallbackRecords *[]byte
	Records         *[]byte
}

func (e DelegateStateExists) Destroy() {
	FfiDestroyerBytes{}.Destroy(e.ScriptPubkey)
	FfiDestroyerOptionalBytes{}.Destroy(e.FallbackRecords)
	FfiDestroyerOptionalBytes{}.Destroy(e.Records)
}

type DelegateStateEmpty struct {
}

func (e DelegateStateEmpty) Destroy() {
}

type DelegateStateUnknown struct {
}

func (e DelegateStateUnknown) Destroy() {
}

type FfiConverterDelegateState struct{}

var FfiConverterDelegateStateINSTANCE = FfiConverterDelegateState{}

func (c FfiConverterDelegateState) Lift(rb RustBufferI) DelegateState {
	return LiftFromRustBuffer[DelegateState](c, rb)
}

func (c FfiConverterDelegateState) Lower(value DelegateState) C.RustBuffer {
	return LowerIntoRustBuffer[DelegateState](c, value)
}

func (c FfiConverterDelegateState) LowerExternal(value DelegateState) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[DelegateState](c, value))
}
func (FfiConverterDelegateState) Read(reader io.Reader) DelegateState {
	id := readInt32(reader)
	switch id {
	case 1:
		return DelegateStateExists{
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
		}
	case 2:
		return DelegateStateEmpty{}
	case 3:
		return DelegateStateUnknown{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterDelegateState.Read()", id))
	}
}

func (FfiConverterDelegateState) Write(writer io.Writer, value DelegateState) {
	switch variant_value := value.(type) {
	case DelegateStateExists:
		writeInt32(writer, 1)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.ScriptPubkey)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.FallbackRecords)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.Records)
	case DelegateStateEmpty:
		writeInt32(writer, 2)
	case DelegateStateUnknown:
		writeInt32(writer, 3)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterDelegateState.Write", value))
	}
}

type FfiDestroyerDelegateState struct{}

func (_ FfiDestroyerDelegateState) Destroy(value DelegateState) {
	value.Destroy()
}

// A single SIP-7 record.
type Record interface {
	Destroy()
}
type RecordSeq struct {
	Version uint64
}

func (e RecordSeq) Destroy() {
	FfiDestroyerUint64{}.Destroy(e.Version)
}

type RecordTxt struct {
	Key   string
	Value string
}

func (e RecordTxt) Destroy() {
	FfiDestroyerString{}.Destroy(e.Key)
	FfiDestroyerString{}.Destroy(e.Value)
}

type RecordBlob struct {
	Key   string
	Value []byte
}

func (e RecordBlob) Destroy() {
	FfiDestroyerString{}.Destroy(e.Key)
	FfiDestroyerBytes{}.Destroy(e.Value)
}

type RecordUnknown struct {
	Rtype uint8
	Rdata []byte
}

func (e RecordUnknown) Destroy() {
	FfiDestroyerUint8{}.Destroy(e.Rtype)
	FfiDestroyerBytes{}.Destroy(e.Rdata)
}

type FfiConverterRecord struct{}

var FfiConverterRecordINSTANCE = FfiConverterRecord{}

func (c FfiConverterRecord) Lift(rb RustBufferI) Record {
	return LiftFromRustBuffer[Record](c, rb)
}

func (c FfiConverterRecord) Lower(value Record) C.RustBuffer {
	return LowerIntoRustBuffer[Record](c, value)
}

func (c FfiConverterRecord) LowerExternal(value Record) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[Record](c, value))
}
func (FfiConverterRecord) Read(reader io.Reader) Record {
	id := readInt32(reader)
	switch id {
	case 1:
		return RecordSeq{
			FfiConverterUint64INSTANCE.Read(reader),
		}
	case 2:
		return RecordTxt{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterStringINSTANCE.Read(reader),
		}
	case 3:
		return RecordBlob{
			FfiConverterStringINSTANCE.Read(reader),
			FfiConverterBytesINSTANCE.Read(reader),
		}
	case 4:
		return RecordUnknown{
			FfiConverterUint8INSTANCE.Read(reader),
			FfiConverterBytesINSTANCE.Read(reader),
		}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterRecord.Read()", id))
	}
}

func (FfiConverterRecord) Write(writer io.Writer, value Record) {
	switch variant_value := value.(type) {
	case RecordSeq:
		writeInt32(writer, 1)
		FfiConverterUint64INSTANCE.Write(writer, variant_value.Version)
	case RecordTxt:
		writeInt32(writer, 2)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Key)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Value)
	case RecordBlob:
		writeInt32(writer, 3)
		FfiConverterStringINSTANCE.Write(writer, variant_value.Key)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.Value)
	case RecordUnknown:
		writeInt32(writer, 4)
		FfiConverterUint8INSTANCE.Write(writer, variant_value.Rtype)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.Rdata)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterRecord.Write", value))
	}
}

type FfiDestroyerRecord struct{}

func (_ FfiDestroyerRecord) Destroy(value Record) {
	value.Destroy()
}

type VeritasError struct {
	err error
}

// Convience method to turn *VeritasError into error
// Avoiding treating nil pointer as non nil error interface
func (err *VeritasError) AsError() error {
	if err == nil {
		return nil
	} else {
		return err
	}
}

func (err VeritasError) Error() string {
	return fmt.Sprintf("VeritasError: %s", err.err.Error())
}

func (err VeritasError) Unwrap() error {
	return err.err
}

// Err* are used for checking error type with `errors.Is`
var ErrVeritasErrorInvalidInput = fmt.Errorf("VeritasErrorInvalidInput")
var ErrVeritasErrorVerificationFailed = fmt.Errorf("VeritasErrorVerificationFailed")

// Variant structs
type VeritasErrorInvalidInput struct {
	Msg string
}

func NewVeritasErrorInvalidInput(
	msg string,
) *VeritasError {
	return &VeritasError{err: &VeritasErrorInvalidInput{
		Msg: msg}}
}

func (e VeritasErrorInvalidInput) destroy() {
	FfiDestroyerString{}.Destroy(e.Msg)
}

func (err VeritasErrorInvalidInput) Error() string {
	return fmt.Sprint("InvalidInput",
		": ",

		"Msg=",
		err.Msg,
	)
}

func (self VeritasErrorInvalidInput) Is(target error) bool {
	return target == ErrVeritasErrorInvalidInput
}

type VeritasErrorVerificationFailed struct {
	Msg string
}

func NewVeritasErrorVerificationFailed(
	msg string,
) *VeritasError {
	return &VeritasError{err: &VeritasErrorVerificationFailed{
		Msg: msg}}
}

func (e VeritasErrorVerificationFailed) destroy() {
	FfiDestroyerString{}.Destroy(e.Msg)
}

func (err VeritasErrorVerificationFailed) Error() string {
	return fmt.Sprint("VerificationFailed",
		": ",

		"Msg=",
		err.Msg,
	)
}

func (self VeritasErrorVerificationFailed) Is(target error) bool {
	return target == ErrVeritasErrorVerificationFailed
}

type FfiConverterVeritasError struct{}

var FfiConverterVeritasErrorINSTANCE = FfiConverterVeritasError{}

func (c FfiConverterVeritasError) Lift(eb RustBufferI) *VeritasError {
	return LiftFromRustBuffer[*VeritasError](c, eb)
}

func (c FfiConverterVeritasError) Lower(value *VeritasError) C.RustBuffer {
	return LowerIntoRustBuffer[*VeritasError](c, value)
}

func (c FfiConverterVeritasError) LowerExternal(value *VeritasError) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*VeritasError](c, value))
}

func (c FfiConverterVeritasError) Read(reader io.Reader) *VeritasError {
	errorID := readUint32(reader)

	switch errorID {
	case 1:
		return &VeritasError{&VeritasErrorInvalidInput{
			Msg: FfiConverterStringINSTANCE.Read(reader),
		}}
	case 2:
		return &VeritasError{&VeritasErrorVerificationFailed{
			Msg: FfiConverterStringINSTANCE.Read(reader),
		}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterVeritasError.Read()", errorID))
	}
}

func (c FfiConverterVeritasError) Write(writer io.Writer, value *VeritasError) {
	switch variantValue := value.err.(type) {
	case *VeritasErrorInvalidInput:
		writeInt32(writer, 1)
		FfiConverterStringINSTANCE.Write(writer, variantValue.Msg)
	case *VeritasErrorVerificationFailed:
		writeInt32(writer, 2)
		FfiConverterStringINSTANCE.Write(writer, variantValue.Msg)
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiConverterVeritasError.Write", value))
	}
}

type FfiDestroyerVeritasError struct{}

func (_ FfiDestroyerVeritasError) Destroy(value *VeritasError) {
	switch variantValue := value.err.(type) {
	case VeritasErrorInvalidInput:
		variantValue.destroy()
	case VeritasErrorVerificationFailed:
		variantValue.destroy()
	default:
		_ = variantValue
		panic(fmt.Sprintf("invalid error value `%v` in FfiDestroyerVeritasError.Destroy", value))
	}
}

type FfiConverterOptionalString struct{}

var FfiConverterOptionalStringINSTANCE = FfiConverterOptionalString{}

func (c FfiConverterOptionalString) Lift(rb RustBufferI) *string {
	return LiftFromRustBuffer[*string](c, rb)
}

func (_ FfiConverterOptionalString) Read(reader io.Reader) *string {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterStringINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalString) Lower(value *string) C.RustBuffer {
	return LowerIntoRustBuffer[*string](c, value)
}

func (c FfiConverterOptionalString) LowerExternal(value *string) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*string](c, value))
}

func (_ FfiConverterOptionalString) Write(writer io.Writer, value *string) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterStringINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalString struct{}

func (_ FfiDestroyerOptionalString) Destroy(value *string) {
	if value != nil {
		FfiDestroyerString{}.Destroy(*value)
	}
}

type FfiConverterOptionalBytes struct{}

var FfiConverterOptionalBytesINSTANCE = FfiConverterOptionalBytes{}

func (c FfiConverterOptionalBytes) Lift(rb RustBufferI) *[]byte {
	return LiftFromRustBuffer[*[]byte](c, rb)
}

func (_ FfiConverterOptionalBytes) Read(reader io.Reader) *[]byte {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterBytesINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalBytes) Lower(value *[]byte) C.RustBuffer {
	return LowerIntoRustBuffer[*[]byte](c, value)
}

func (c FfiConverterOptionalBytes) LowerExternal(value *[]byte) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*[]byte](c, value))
}

func (_ FfiConverterOptionalBytes) Write(writer io.Writer, value *[]byte) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterBytesINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalBytes struct{}

func (_ FfiDestroyerOptionalBytes) Destroy(value *[]byte) {
	if value != nil {
		FfiDestroyerBytes{}.Destroy(*value)
	}
}

type FfiConverterOptionalCertificate struct{}

var FfiConverterOptionalCertificateINSTANCE = FfiConverterOptionalCertificate{}

func (c FfiConverterOptionalCertificate) Lift(rb RustBufferI) *Certificate {
	return LiftFromRustBuffer[*Certificate](c, rb)
}

func (_ FfiConverterOptionalCertificate) Read(reader io.Reader) *Certificate {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterCertificateINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalCertificate) Lower(value *Certificate) C.RustBuffer {
	return LowerIntoRustBuffer[*Certificate](c, value)
}

func (c FfiConverterOptionalCertificate) LowerExternal(value *Certificate) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[*Certificate](c, value))
}

func (_ FfiConverterOptionalCertificate) Write(writer io.Writer, value *Certificate) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterCertificateINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalCertificate struct{}

func (_ FfiDestroyerOptionalCertificate) Destroy(value *Certificate) {
	if value != nil {
		FfiDestroyerCertificate{}.Destroy(*value)
	}
}

type FfiConverterSequenceString struct{}

var FfiConverterSequenceStringINSTANCE = FfiConverterSequenceString{}

func (c FfiConverterSequenceString) Lift(rb RustBufferI) []string {
	return LiftFromRustBuffer[[]string](c, rb)
}

func (c FfiConverterSequenceString) Read(reader io.Reader) []string {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]string, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterStringINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceString) Lower(value []string) C.RustBuffer {
	return LowerIntoRustBuffer[[]string](c, value)
}

func (c FfiConverterSequenceString) LowerExternal(value []string) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]string](c, value))
}

func (c FfiConverterSequenceString) Write(writer io.Writer, value []string) {
	if len(value) > math.MaxInt32 {
		panic("[]string is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterStringINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceString struct{}

func (FfiDestroyerSequenceString) Destroy(sequence []string) {
	for _, value := range sequence {
		FfiDestroyerString{}.Destroy(value)
	}
}

type FfiConverterSequenceBytes struct{}

var FfiConverterSequenceBytesINSTANCE = FfiConverterSequenceBytes{}

func (c FfiConverterSequenceBytes) Lift(rb RustBufferI) [][]byte {
	return LiftFromRustBuffer[[][]byte](c, rb)
}

func (c FfiConverterSequenceBytes) Read(reader io.Reader) [][]byte {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([][]byte, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterBytesINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceBytes) Lower(value [][]byte) C.RustBuffer {
	return LowerIntoRustBuffer[[][]byte](c, value)
}

func (c FfiConverterSequenceBytes) LowerExternal(value [][]byte) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[][]byte](c, value))
}

func (c FfiConverterSequenceBytes) Write(writer io.Writer, value [][]byte) {
	if len(value) > math.MaxInt32 {
		panic("[][]byte is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterBytesINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceBytes struct{}

func (FfiDestroyerSequenceBytes) Destroy(sequence [][]byte) {
	for _, value := range sequence {
		FfiDestroyerBytes{}.Destroy(value)
	}
}

type FfiConverterSequenceCertificate struct{}

var FfiConverterSequenceCertificateINSTANCE = FfiConverterSequenceCertificate{}

func (c FfiConverterSequenceCertificate) Lift(rb RustBufferI) []Certificate {
	return LiftFromRustBuffer[[]Certificate](c, rb)
}

func (c FfiConverterSequenceCertificate) Read(reader io.Reader) []Certificate {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Certificate, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterCertificateINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceCertificate) Lower(value []Certificate) C.RustBuffer {
	return LowerIntoRustBuffer[[]Certificate](c, value)
}

func (c FfiConverterSequenceCertificate) LowerExternal(value []Certificate) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Certificate](c, value))
}

func (c FfiConverterSequenceCertificate) Write(writer io.Writer, value []Certificate) {
	if len(value) > math.MaxInt32 {
		panic("[]Certificate is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterCertificateINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceCertificate struct{}

func (FfiDestroyerSequenceCertificate) Destroy(sequence []Certificate) {
	for _, value := range sequence {
		FfiDestroyerCertificate{}.Destroy(value)
	}
}

type FfiConverterSequenceDataUpdateEntry struct{}

var FfiConverterSequenceDataUpdateEntryINSTANCE = FfiConverterSequenceDataUpdateEntry{}

func (c FfiConverterSequenceDataUpdateEntry) Lift(rb RustBufferI) []DataUpdateEntry {
	return LiftFromRustBuffer[[]DataUpdateEntry](c, rb)
}

func (c FfiConverterSequenceDataUpdateEntry) Read(reader io.Reader) []DataUpdateEntry {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]DataUpdateEntry, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterDataUpdateEntryINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceDataUpdateEntry) Lower(value []DataUpdateEntry) C.RustBuffer {
	return LowerIntoRustBuffer[[]DataUpdateEntry](c, value)
}

func (c FfiConverterSequenceDataUpdateEntry) LowerExternal(value []DataUpdateEntry) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]DataUpdateEntry](c, value))
}

func (c FfiConverterSequenceDataUpdateEntry) Write(writer io.Writer, value []DataUpdateEntry) {
	if len(value) > math.MaxInt32 {
		panic("[]DataUpdateEntry is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterDataUpdateEntryINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceDataUpdateEntry struct{}

func (FfiDestroyerSequenceDataUpdateEntry) Destroy(sequence []DataUpdateEntry) {
	for _, value := range sequence {
		FfiDestroyerDataUpdateEntry{}.Destroy(value)
	}
}

type FfiConverterSequenceZone struct{}

var FfiConverterSequenceZoneINSTANCE = FfiConverterSequenceZone{}

func (c FfiConverterSequenceZone) Lift(rb RustBufferI) []Zone {
	return LiftFromRustBuffer[[]Zone](c, rb)
}

func (c FfiConverterSequenceZone) Read(reader io.Reader) []Zone {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Zone, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterZoneINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceZone) Lower(value []Zone) C.RustBuffer {
	return LowerIntoRustBuffer[[]Zone](c, value)
}

func (c FfiConverterSequenceZone) LowerExternal(value []Zone) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Zone](c, value))
}

func (c FfiConverterSequenceZone) Write(writer io.Writer, value []Zone) {
	if len(value) > math.MaxInt32 {
		panic("[]Zone is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterZoneINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceZone struct{}

func (FfiDestroyerSequenceZone) Destroy(sequence []Zone) {
	for _, value := range sequence {
		FfiDestroyerZone{}.Destroy(value)
	}
}

type FfiConverterSequenceRecord struct{}

var FfiConverterSequenceRecordINSTANCE = FfiConverterSequenceRecord{}

func (c FfiConverterSequenceRecord) Lift(rb RustBufferI) []Record {
	return LiftFromRustBuffer[[]Record](c, rb)
}

func (c FfiConverterSequenceRecord) Read(reader io.Reader) []Record {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]Record, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterRecordINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceRecord) Lower(value []Record) C.RustBuffer {
	return LowerIntoRustBuffer[[]Record](c, value)
}

func (c FfiConverterSequenceRecord) LowerExternal(value []Record) ExternalCRustBuffer {
	return RustBufferFromC(LowerIntoRustBuffer[[]Record](c, value))
}

func (c FfiConverterSequenceRecord) Write(writer io.Writer, value []Record) {
	if len(value) > math.MaxInt32 {
		panic("[]Record is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterRecordINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceRecord struct{}

func (FfiDestroyerSequenceRecord) Destroy(sequence []Record) {
	for _, value := range sequence {
		FfiDestroyerRecord{}.Destroy(value)
	}
}

// Create a .spacecert file from a subject name and certificate bytes.
func CreateCertificateChain(subject string, certBytesList [][]byte) ([]byte, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_create_certificate_chain(FfiConverterStringINSTANCE.Lower(subject), FfiConverterSequenceBytesINSTANCE.Lower(certBytesList), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []byte
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBytesINSTANCE.Lift(_uniffiRV), nil
	}
}

// Create borsh-encoded OffchainRecords from a RecordSet and 64-byte Schnorr signature.
func CreateOffchainRecords(recordSet *RecordSet, signature []byte) ([]byte, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_create_offchain_records(FfiConverterRecordSetINSTANCE.Lower(recordSet), FfiConverterBytesINSTANCE.Lower(signature), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []byte
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBytesINSTANCE.Lift(_uniffiRV), nil
	}
}

// Decode stored certificate bytes to JSON.
func DecodeCertificate(bytes []byte) (string, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_decode_certificate(FfiConverterBytesINSTANCE.Lower(bytes), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}

// Decode stored zone bytes to a Zone record.
func DecodeZone(bytes []byte) (Zone, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_decode_zone(FfiConverterBytesINSTANCE.Lower(bytes), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue Zone
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterZoneINSTANCE.Lift(_uniffiRV), nil
	}
}

// Hash a message with the Spaces signed-message prefix (SHA256).
// Returns the 32-byte digest suitable for Schnorr signing/verification.
func HashSignableMessage(msg []byte) []byte {
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_hash_signable_message(FfiConverterBytesINSTANCE.Lower(msg), _uniffiStatus),
		}
	}))
}

func VerifyDefault() uint32 {
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_func_verify_default(_uniffiStatus)
	}))
}

func VerifyDevMode() uint32 {
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_func_verify_dev_mode(_uniffiStatus)
	}))
}

func VerifyEnableSnark() uint32 {
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_func_verify_enable_snark(_uniffiStatus)
	}))
}

// Verify a raw Schnorr signature (no prefix, caller provides the 32-byte message hash).
//
// - `msg_hash`: 32-byte SHA256 hash
// - `signature`: 64-byte Schnorr signature
// - `pubkey`: 32-byte x-only public key
func VerifySchnorr(msgHash []byte, signature []byte, pubkey []byte) error {
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_func_verify_schnorr(FfiConverterBytesINSTANCE.Lower(msgHash), FfiConverterBytesINSTANCE.Lower(signature), FfiConverterBytesINSTANCE.Lower(pubkey), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Verify a Schnorr signature over a message using the Spaces signed-message prefix.
//
// - `msg`: raw message bytes (prefixed and hashed internally)
// - `signature`: 64-byte Schnorr signature
// - `pubkey`: 32-byte x-only public key
func VerifySpacesMessage(msg []byte, signature []byte, pubkey []byte) error {
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_func_verify_spaces_message(FfiConverterBytesINSTANCE.Lower(msg), FfiConverterBytesINSTANCE.Lower(signature), FfiConverterBytesINSTANCE.Lower(pubkey), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Compare two zones — returns true if `a` is fresher/better than `b`.
func ZoneIsBetterThan(a Zone, b Zone) (bool, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_libveritas_uniffi_fn_func_zone_is_better_than(FfiConverterZoneINSTANCE.Lower(a), FfiConverterZoneINSTANCE.Lower(b), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

// Serialize a Zone record to borsh bytes for storage.
func ZoneToBytes(zone Zone) ([]byte, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_zone_to_bytes(FfiConverterZoneINSTANCE.Lower(zone), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue []byte
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBytesINSTANCE.Lift(_uniffiRV), nil
	}
}

// Serialize a Zone record to JSON.
func ZoneToJson(zone Zone) (string, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_zone_to_json(FfiConverterZoneINSTANCE.Lower(zone), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
	}
}
