package libveritas_uniffi

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

func RustBufferFromExternal(b RustBufferI) GoRustBuffer {
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
	bindingsContractVersion := 26
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
			return C.uniffi_libveritas_uniffi_checksum_func_create_offchain_data()
		})
		if checksum != 28866 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_func_create_offchain_data: UniFFI API checksum mismatch")
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
		if checksum != 59384 {
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
		if checksum != 45160 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_message_update: UniFFI API checksum mismatch")
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
			return C.uniffi_libveritas_uniffi_checksum_method_recordset_id()
		})
		if checksum != 62743 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_recordset_id: UniFFI API checksum mismatch")
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
		if checksum != 51808 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_zones: UniFFI API checksum mismatch")
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
			return C.uniffi_libveritas_uniffi_checksum_method_veritas_verify_message()
		})
		if checksum != 55323 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_verify_message: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_anchor()
		})
		if checksum != 50943 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_anchor: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_commitment()
		})
		if checksum != 3594 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_commitment: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_data()
		})
		if checksum != 65114 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_data: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_delegate()
		})
		if checksum != 56633 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_delegate: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_handle()
		})
		if checksum != 37290 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_handle: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_is_better_than()
		})
		if checksum != 46722 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_is_better_than: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_offchain_data()
		})
		if checksum != 34636 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_offchain_data: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_script_pubkey()
		})
		if checksum != 64238 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_script_pubkey: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_sovereignty()
		})
		if checksum != 7046 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_sovereignty: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_to_bytes()
		})
		if checksum != 42046 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_to_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_zone_to_json()
		})
		if checksum != 38582 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_zone_to_json: UniFFI API checksum mismatch")
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
			return C.uniffi_libveritas_uniffi_checksum_constructor_message_from_bytes()
		})
		if checksum != 9710 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_message_from_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_messagebuilder_new()
		})
		if checksum != 13656 {
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
		if checksum != 26032 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_recordset_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_veritas_new()
		})
		if checksum != 32872 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_veritas_new: UniFFI API checksum mismatch")
		}
	}
}

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

type MessageInterface interface {
	// Serialize the message to borsh bytes.
	ToBytes() []byte
	// Update offchain data and/or root certificates on this message.
	//
	// - `name`: handle string (e.g. "alice@bitcoin", "@bitcoin", "#12-12")
	// - `offchain_data`: borsh-encoded OffchainData (optional)
	// - `delegate_offchain_data`: borsh-encoded OffchainData (optional)
	// - `cert`: borsh-encoded Certificate (optional, root only — for receipt refresh)
	Update(updates []DataUpdateEntry) error
}
type Message struct {
	ffiObject FfiObject
}

// Decode a message from borsh bytes.
func MessageFromBytes(bytes []byte) (*Message, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_message_from_bytes(FfiConverterBytesINSTANCE.Lower(bytes), _uniffiStatus)
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

// Update offchain data and/or root certificates on this message.
//
// - `name`: handle string (e.g. "alice@bitcoin", "@bitcoin", "#12-12")
// - `offchain_data`: borsh-encoded OffchainData (optional)
// - `delegate_offchain_data`: borsh-encoded OffchainData (optional)
// - `cert`: borsh-encoded Certificate (optional, root only — for receipt refresh)
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

// Create a builder from a list of update entries.
func NewMessageBuilder(requests []UpdateEntry) (*MessageBuilder, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_messagebuilder_new(FfiConverterSequenceUpdateEntryINSTANCE.Lower(requests), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *MessageBuilder
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterMessageBuilderINSTANCE.Lift(_uniffiRV), nil
	}
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

// Serialized records ready to be signed.
type RecordSetInterface interface {
	// The 32-byte hash to sign.
	Id() []byte
}

// Serialized records ready to be signed.
type RecordSet struct {
	ffiObject FfiObject
}

// Create a record set from a sequence number and a JSON string of key-value pairs.
//
// Records are sorted by key for deterministic serialization.
// Example: `'{"nostr":"npub1...","ipv4":"127.0.0.1"}'`
func NewRecordSet(seq uint32, recordsJson string) (*RecordSet, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_recordset_new(FfiConverterUint32INSTANCE.Lower(seq), FfiConverterStringINSTANCE.Lower(recordsJson), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *RecordSet
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterRecordSetINSTANCE.Lift(_uniffiRV), nil
	}
}

// The 32-byte hash to sign.
func (_self *RecordSet) Id() []byte {
	_pointer := _self.ffiObject.incrementPointer("*RecordSet")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_recordset_id(
				_pointer, _uniffiStatus),
		}
	}))
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
	Zones() []*Zone
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

func (_self *VerifiedMessage) Zones() []*Zone {
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
	IsFinalized(commitmentHeight uint32) bool
	NewestAnchor() uint32
	OldestAnchor() uint32
	SovereigntyFor(commitmentHeight uint32) string
	// Verify an encoded message against a query context.
	VerifyMessage(ctx *QueryContext, msg []byte) (*VerifiedMessage, error)
}
type Veritas struct {
	ffiObject FfiObject
}

func NewVeritas(anchors *Anchors, devMode bool) (*Veritas, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_veritas_new(FfiConverterAnchorsINSTANCE.Lower(anchors), FfiConverterBoolINSTANCE.Lower(devMode), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *Veritas
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterVeritasINSTANCE.Lift(_uniffiRV), nil
	}
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

// Verify an encoded message against a query context.
func (_self *Veritas) VerifyMessage(ctx *QueryContext, msg []byte) (*VerifiedMessage, error) {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_verify_message(
			_pointer, FfiConverterQueryContextINSTANCE.Lower(ctx), FfiConverterBytesINSTANCE.Lower(msg), _uniffiStatus)
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

type ZoneInterface interface {
	Anchor() uint32
	Commitment() CommitmentState
	Data() *[]byte
	Delegate() DelegateState
	Handle() string
	IsBetterThan(other *Zone) (bool, error)
	OffchainData() *OffchainRecord
	ScriptPubkey() []byte
	Sovereignty() string
	ToBytes() []byte
	ToJson() (string, error)
}
type Zone struct {
	ffiObject FfiObject
}

func (_self *Zone) Anchor() uint32 {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_method_zone_anchor(
			_pointer, _uniffiStatus)
	}))
}

func (_self *Zone) Commitment() CommitmentState {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterCommitmentStateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_commitment(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) Data() *[]byte {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterOptionalBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_data(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) Delegate() DelegateState {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterDelegateStateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_delegate(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) Handle() string {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_handle(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) IsBetterThan(other *Zone) (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_libveritas_uniffi_fn_method_zone_is_better_than(
			_pointer, FfiConverterZoneINSTANCE.Lower(other), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *Zone) OffchainData() *OffchainRecord {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterOptionalOffchainRecordINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_offchain_data(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) ScriptPubkey() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_script_pubkey(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) Sovereignty() string {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_sovereignty(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) ToBytes() []byte {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_to_bytes(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *Zone) ToJson() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*Zone")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_zone_to_json(
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
func (object *Zone) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterZone struct{}

var FfiConverterZoneINSTANCE = FfiConverterZone{}

func (c FfiConverterZone) Lift(pointer unsafe.Pointer) *Zone {
	result := &Zone{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_zone(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_zone(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*Zone).Destroy)
	return result
}

func (c FfiConverterZone) Read(reader io.Reader) *Zone {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterZone) Lower(value *Zone) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*Zone")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterZone) Write(writer io.Writer, value *Zone) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerZone struct{}

func (_ FfiDestroyerZone) Destroy(value *Zone) {
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
	Name                 string
	OffchainData         *[]byte
	DelegateOffchainData *[]byte
}

func (r *DataUpdateEntry) Destroy() {
	FfiDestroyerString{}.Destroy(r.Name)
	FfiDestroyerOptionalBytes{}.Destroy(r.OffchainData)
	FfiDestroyerOptionalBytes{}.Destroy(r.DelegateOffchainData)
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

func (c FfiConverterDataUpdateEntry) Write(writer io.Writer, value DataUpdateEntry) {
	FfiConverterStringINSTANCE.Write(writer, value.Name)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.OffchainData)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.DelegateOffchainData)
}

type FfiDestroyerDataUpdateEntry struct{}

func (_ FfiDestroyerDataUpdateEntry) Destroy(value DataUpdateEntry) {
	value.Destroy()
}

type OffchainRecord struct {
	Seq  uint32
	Data []byte
}

func (r *OffchainRecord) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Seq)
	FfiDestroyerBytes{}.Destroy(r.Data)
}

type FfiConverterOffchainRecord struct{}

var FfiConverterOffchainRecordINSTANCE = FfiConverterOffchainRecord{}

func (c FfiConverterOffchainRecord) Lift(rb RustBufferI) OffchainRecord {
	return LiftFromRustBuffer[OffchainRecord](c, rb)
}

func (c FfiConverterOffchainRecord) Read(reader io.Reader) OffchainRecord {
	return OffchainRecord{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterOffchainRecord) Lower(value OffchainRecord) C.RustBuffer {
	return LowerIntoRustBuffer[OffchainRecord](c, value)
}

func (c FfiConverterOffchainRecord) Write(writer io.Writer, value OffchainRecord) {
	FfiConverterUint32INSTANCE.Write(writer, value.Seq)
	FfiConverterBytesINSTANCE.Write(writer, value.Data)
}

type FfiDestroyerOffchainRecord struct{}

func (_ FfiDestroyerOffchainRecord) Destroy(value OffchainRecord) {
	value.Destroy()
}

// Update entry for MessageBuilder — includes optional cert.
type UpdateEntry struct {
	Name                 string
	OffchainData         *[]byte
	DelegateOffchainData *[]byte
	Cert                 *[]byte
}

func (r *UpdateEntry) Destroy() {
	FfiDestroyerString{}.Destroy(r.Name)
	FfiDestroyerOptionalBytes{}.Destroy(r.OffchainData)
	FfiDestroyerOptionalBytes{}.Destroy(r.DelegateOffchainData)
	FfiDestroyerOptionalBytes{}.Destroy(r.Cert)
}

type FfiConverterUpdateEntry struct{}

var FfiConverterUpdateEntryINSTANCE = FfiConverterUpdateEntry{}

func (c FfiConverterUpdateEntry) Lift(rb RustBufferI) UpdateEntry {
	return LiftFromRustBuffer[UpdateEntry](c, rb)
}

func (c FfiConverterUpdateEntry) Read(reader io.Reader) UpdateEntry {
	return UpdateEntry{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
		FfiConverterOptionalBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterUpdateEntry) Lower(value UpdateEntry) C.RustBuffer {
	return LowerIntoRustBuffer[UpdateEntry](c, value)
}

func (c FfiConverterUpdateEntry) Write(writer io.Writer, value UpdateEntry) {
	FfiConverterStringINSTANCE.Write(writer, value.Name)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.OffchainData)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.DelegateOffchainData)
	FfiConverterOptionalBytesINSTANCE.Write(writer, value.Cert)
}

type FfiDestroyerUpdateEntry struct{}

func (_ FfiDestroyerUpdateEntry) Destroy(value UpdateEntry) {
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
	ScriptPubkey []byte
	Data         *[]byte
	OffchainData *OffchainRecord
}

func (e DelegateStateExists) Destroy() {
	FfiDestroyerBytes{}.Destroy(e.ScriptPubkey)
	FfiDestroyerOptionalBytes{}.Destroy(e.Data)
	FfiDestroyerOptionalOffchainRecord{}.Destroy(e.OffchainData)
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
func (FfiConverterDelegateState) Read(reader io.Reader) DelegateState {
	id := readInt32(reader)
	switch id {
	case 1:
		return DelegateStateExists{
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
			FfiConverterOptionalOffchainRecordINSTANCE.Read(reader),
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
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.Data)
		FfiConverterOptionalOffchainRecordINSTANCE.Write(writer, variant_value.OffchainData)
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
	Message string
}

func NewVeritasErrorInvalidInput(
	message string,
) *VeritasError {
	return &VeritasError{err: &VeritasErrorInvalidInput{
		Message: message}}
}

func (e VeritasErrorInvalidInput) destroy() {
	FfiDestroyerString{}.Destroy(e.Message)
}

func (err VeritasErrorInvalidInput) Error() string {
	return fmt.Sprint("InvalidInput",
		": ",

		"Message=",
		err.Message,
	)
}

func (self VeritasErrorInvalidInput) Is(target error) bool {
	return target == ErrVeritasErrorInvalidInput
}

type VeritasErrorVerificationFailed struct {
	Message string
}

func NewVeritasErrorVerificationFailed(
	message string,
) *VeritasError {
	return &VeritasError{err: &VeritasErrorVerificationFailed{
		Message: message}}
}

func (e VeritasErrorVerificationFailed) destroy() {
	FfiDestroyerString{}.Destroy(e.Message)
}

func (err VeritasErrorVerificationFailed) Error() string {
	return fmt.Sprint("VerificationFailed",
		": ",

		"Message=",
		err.Message,
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

func (c FfiConverterVeritasError) Read(reader io.Reader) *VeritasError {
	errorID := readUint32(reader)

	switch errorID {
	case 1:
		return &VeritasError{&VeritasErrorInvalidInput{
			Message: FfiConverterStringINSTANCE.Read(reader),
		}}
	case 2:
		return &VeritasError{&VeritasErrorVerificationFailed{
			Message: FfiConverterStringINSTANCE.Read(reader),
		}}
	default:
		panic(fmt.Sprintf("Unknown error code %d in FfiConverterVeritasError.Read()", errorID))
	}
}

func (c FfiConverterVeritasError) Write(writer io.Writer, value *VeritasError) {
	switch variantValue := value.err.(type) {
	case *VeritasErrorInvalidInput:
		writeInt32(writer, 1)
		FfiConverterStringINSTANCE.Write(writer, variantValue.Message)
	case *VeritasErrorVerificationFailed:
		writeInt32(writer, 2)
		FfiConverterStringINSTANCE.Write(writer, variantValue.Message)
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

type FfiConverterOptionalOffchainRecord struct{}

var FfiConverterOptionalOffchainRecordINSTANCE = FfiConverterOptionalOffchainRecord{}

func (c FfiConverterOptionalOffchainRecord) Lift(rb RustBufferI) *OffchainRecord {
	return LiftFromRustBuffer[*OffchainRecord](c, rb)
}

func (_ FfiConverterOptionalOffchainRecord) Read(reader io.Reader) *OffchainRecord {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterOffchainRecordINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalOffchainRecord) Lower(value *OffchainRecord) C.RustBuffer {
	return LowerIntoRustBuffer[*OffchainRecord](c, value)
}

func (_ FfiConverterOptionalOffchainRecord) Write(writer io.Writer, value *OffchainRecord) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterOffchainRecordINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalOffchainRecord struct{}

func (_ FfiDestroyerOptionalOffchainRecord) Destroy(value *OffchainRecord) {
	if value != nil {
		FfiDestroyerOffchainRecord{}.Destroy(*value)
	}
}

type FfiConverterSequenceZone struct{}

var FfiConverterSequenceZoneINSTANCE = FfiConverterSequenceZone{}

func (c FfiConverterSequenceZone) Lift(rb RustBufferI) []*Zone {
	return LiftFromRustBuffer[[]*Zone](c, rb)
}

func (c FfiConverterSequenceZone) Read(reader io.Reader) []*Zone {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]*Zone, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterZoneINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceZone) Lower(value []*Zone) C.RustBuffer {
	return LowerIntoRustBuffer[[]*Zone](c, value)
}

func (c FfiConverterSequenceZone) Write(writer io.Writer, value []*Zone) {
	if len(value) > math.MaxInt32 {
		panic("[]*Zone is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterZoneINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceZone struct{}

func (FfiDestroyerSequenceZone) Destroy(sequence []*Zone) {
	for _, value := range sequence {
		FfiDestroyerZone{}.Destroy(value)
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

type FfiConverterSequenceUpdateEntry struct{}

var FfiConverterSequenceUpdateEntryINSTANCE = FfiConverterSequenceUpdateEntry{}

func (c FfiConverterSequenceUpdateEntry) Lift(rb RustBufferI) []UpdateEntry {
	return LiftFromRustBuffer[[]UpdateEntry](c, rb)
}

func (c FfiConverterSequenceUpdateEntry) Read(reader io.Reader) []UpdateEntry {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]UpdateEntry, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterUpdateEntryINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceUpdateEntry) Lower(value []UpdateEntry) C.RustBuffer {
	return LowerIntoRustBuffer[[]UpdateEntry](c, value)
}

func (c FfiConverterSequenceUpdateEntry) Write(writer io.Writer, value []UpdateEntry) {
	if len(value) > math.MaxInt32 {
		panic("[]UpdateEntry is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterUpdateEntryINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceUpdateEntry struct{}

func (FfiDestroyerSequenceUpdateEntry) Destroy(sequence []UpdateEntry) {
	for _, value := range sequence {
		FfiDestroyerUpdateEntry{}.Destroy(value)
	}
}

// Create borsh-encoded OffchainData from a RecordSet and a 64-byte Schnorr signature.
func CreateOffchainData(recordSet *RecordSet, signature []byte) ([]byte, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_create_offchain_data(FfiConverterRecordSetINSTANCE.Lower(recordSet), FfiConverterBytesINSTANCE.Lower(signature), _uniffiStatus),
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

// Decode stored zone bytes to JSON.
func DecodeZone(bytes []byte) (string, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_func_decode_zone(FfiConverterBytesINSTANCE.Lower(bytes), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue string
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterStringINSTANCE.Lift(_uniffiRV), nil
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
