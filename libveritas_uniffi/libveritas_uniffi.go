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
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificate()
		})
		if checksum != 21477 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificate: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificates()
		})
		if checksum != 42989 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_verifiedmessage_certificates: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_verifiedmessage_zones()
		})
		if checksum != 42512 {
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
		if checksum != 27729 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritas_verify_message: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritasquerycontext_add_request()
		})
		if checksum != 54635 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritasquerycontext_add_request: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritasquerycontext_add_zone()
		})
		if checksum != 32901 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritasquerycontext_add_zone: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_anchor()
		})
		if checksum != 15552 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_anchor: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_commitment()
		})
		if checksum != 46659 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_commitment: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_data()
		})
		if checksum != 11175 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_data: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_delegate()
		})
		if checksum != 16818 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_delegate: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_handle()
		})
		if checksum != 27124 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_handle: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_is_better_than()
		})
		if checksum != 11445 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_is_better_than: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_offchain_data()
		})
		if checksum != 13766 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_offchain_data: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_script_pubkey()
		})
		if checksum != 23903 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_script_pubkey: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_sovereignty()
		})
		if checksum != 63614 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_sovereignty: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_to_bytes()
		})
		if checksum != 44775 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_to_bytes: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_method_veritaszone_to_json()
		})
		if checksum != 44380 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_method_veritaszone_to_json: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_veritas_new()
		})
		if checksum != 31646 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_veritas_new: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_veritasanchors_from_json()
		})
		if checksum != 4349 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_veritasanchors_from_json: UniFFI API checksum mismatch")
		}
	}
	{
		checksum := rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint16_t {
			return C.uniffi_libveritas_uniffi_checksum_constructor_veritasquerycontext_new()
		})
		if checksum != 19819 {
			// If this happens try cleaning and rebuilding your project
			panic("libveritas_uniffi: uniffi_libveritas_uniffi_checksum_constructor_veritasquerycontext_new: UniFFI API checksum mismatch")
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

type VerifiedMessageInterface interface {
	Certificate(handle string) (*VeritasCertificate, error)
	Certificates() []VeritasCertificate
	Zones() []*VeritasZone
}
type VerifiedMessage struct {
	ffiObject FfiObject
}

func (_self *VerifiedMessage) Certificate(handle string) (*VeritasCertificate, error) {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_certificate(
				_pointer, FfiConverterStringINSTANCE.Lower(handle), _uniffiStatus),
		}
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *VeritasCertificate
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterOptionalVeritasCertificateINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *VerifiedMessage) Certificates() []VeritasCertificate {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceVeritasCertificateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_verifiedmessage_certificates(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VerifiedMessage) Zones() []*VeritasZone {
	_pointer := _self.ffiObject.incrementPointer("*VerifiedMessage")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterSequenceVeritasZoneINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
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
	VerifyMessage(ctx *VeritasQueryContext, msg []byte) (*VerifiedMessage, error)
}
type Veritas struct {
	ffiObject FfiObject
}

func NewVeritas(anchors *VeritasAnchors, devMode bool) (*Veritas, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_veritas_new(FfiConverterVeritasAnchorsINSTANCE.Lower(anchors), FfiConverterBoolINSTANCE.Lower(devMode), _uniffiStatus)
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
func (_self *Veritas) VerifyMessage(ctx *VeritasQueryContext, msg []byte) (*VerifiedMessage, error) {
	_pointer := _self.ffiObject.incrementPointer("*Veritas")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_method_veritas_verify_message(
			_pointer, FfiConverterVeritasQueryContextINSTANCE.Lower(ctx), FfiConverterBytesINSTANCE.Lower(msg), _uniffiStatus)
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

type VeritasAnchorsInterface interface {
}
type VeritasAnchors struct {
	ffiObject FfiObject
}

func VeritasAnchorsFromJson(json string) (*VeritasAnchors, error) {
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_veritasanchors_from_json(FfiConverterStringINSTANCE.Lower(json), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue *VeritasAnchors
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterVeritasAnchorsINSTANCE.Lift(_uniffiRV), nil
	}
}

func (object *VeritasAnchors) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterVeritasAnchors struct{}

var FfiConverterVeritasAnchorsINSTANCE = FfiConverterVeritasAnchors{}

func (c FfiConverterVeritasAnchors) Lift(pointer unsafe.Pointer) *VeritasAnchors {
	result := &VeritasAnchors{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_veritasanchors(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_veritasanchors(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*VeritasAnchors).Destroy)
	return result
}

func (c FfiConverterVeritasAnchors) Read(reader io.Reader) *VeritasAnchors {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterVeritasAnchors) Lower(value *VeritasAnchors) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*VeritasAnchors")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterVeritasAnchors) Write(writer io.Writer, value *VeritasAnchors) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerVeritasAnchors struct{}

func (_ FfiDestroyerVeritasAnchors) Destroy(value *VeritasAnchors) {
	value.Destroy()
}

type VeritasQueryContextInterface interface {
	// Add a handle to verify (e.g. "alice@bitcoin").
	// If no requests are added, all handles in the message are verified.
	AddRequest(handle string) error
	// Add a known zone from stored bytes (from a previous verification).
	AddZone(zoneBytes []byte) error
}
type VeritasQueryContext struct {
	ffiObject FfiObject
}

func NewVeritasQueryContext() *VeritasQueryContext {
	return FfiConverterVeritasQueryContextINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) unsafe.Pointer {
		return C.uniffi_libveritas_uniffi_fn_constructor_veritasquerycontext_new(_uniffiStatus)
	}))
}

// Add a handle to verify (e.g. "alice@bitcoin").
// If no requests are added, all handles in the message are verified.
func (_self *VeritasQueryContext) AddRequest(handle string) error {
	_pointer := _self.ffiObject.incrementPointer("*VeritasQueryContext")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_veritasquerycontext_add_request(
			_pointer, FfiConverterStringINSTANCE.Lower(handle), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}

// Add a known zone from stored bytes (from a previous verification).
func (_self *VeritasQueryContext) AddZone(zoneBytes []byte) error {
	_pointer := _self.ffiObject.incrementPointer("*VeritasQueryContext")
	defer _self.ffiObject.decrementPointer()
	_, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) bool {
		C.uniffi_libveritas_uniffi_fn_method_veritasquerycontext_add_zone(
			_pointer, FfiConverterBytesINSTANCE.Lower(zoneBytes), _uniffiStatus)
		return false
	})
	return _uniffiErr.AsError()
}
func (object *VeritasQueryContext) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterVeritasQueryContext struct{}

var FfiConverterVeritasQueryContextINSTANCE = FfiConverterVeritasQueryContext{}

func (c FfiConverterVeritasQueryContext) Lift(pointer unsafe.Pointer) *VeritasQueryContext {
	result := &VeritasQueryContext{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_veritasquerycontext(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_veritasquerycontext(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*VeritasQueryContext).Destroy)
	return result
}

func (c FfiConverterVeritasQueryContext) Read(reader io.Reader) *VeritasQueryContext {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterVeritasQueryContext) Lower(value *VeritasQueryContext) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*VeritasQueryContext")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterVeritasQueryContext) Write(writer io.Writer, value *VeritasQueryContext) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerVeritasQueryContext struct{}

func (_ FfiDestroyerVeritasQueryContext) Destroy(value *VeritasQueryContext) {
	value.Destroy()
}

type VeritasZoneInterface interface {
	Anchor() uint32
	Commitment() VeritasCommitmentState
	Data() *[]byte
	Delegate() VeritasDelegateState
	Handle() string
	IsBetterThan(other *VeritasZone) (bool, error)
	OffchainData() *VeritasOffchainData
	ScriptPubkey() []byte
	Sovereignty() string
	ToBytes() []byte
	ToJson() (string, error)
}
type VeritasZone struct {
	ffiObject FfiObject
}

func (_self *VeritasZone) Anchor() uint32 {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterUint32INSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) C.uint32_t {
		return C.uniffi_libveritas_uniffi_fn_method_veritaszone_anchor(
			_pointer, _uniffiStatus)
	}))
}

func (_self *VeritasZone) Commitment() VeritasCommitmentState {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterVeritasCommitmentStateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_commitment(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) Data() *[]byte {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterOptionalBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_data(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) Delegate() VeritasDelegateState {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterVeritasDelegateStateINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_delegate(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) Handle() string {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_handle(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) IsBetterThan(other *VeritasZone) (bool, error) {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) C.int8_t {
		return C.uniffi_libveritas_uniffi_fn_method_veritaszone_is_better_than(
			_pointer, FfiConverterVeritasZoneINSTANCE.Lower(other), _uniffiStatus)
	})
	if _uniffiErr != nil {
		var _uniffiDefaultValue bool
		return _uniffiDefaultValue, _uniffiErr
	} else {
		return FfiConverterBoolINSTANCE.Lift(_uniffiRV), nil
	}
}

func (_self *VeritasZone) OffchainData() *VeritasOffchainData {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterOptionalVeritasOffchainDataINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_offchain_data(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) ScriptPubkey() []byte {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_script_pubkey(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) Sovereignty() string {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterStringINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_sovereignty(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) ToBytes() []byte {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	return FfiConverterBytesINSTANCE.Lift(rustCall(func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_to_bytes(
				_pointer, _uniffiStatus),
		}
	}))
}

func (_self *VeritasZone) ToJson() (string, error) {
	_pointer := _self.ffiObject.incrementPointer("*VeritasZone")
	defer _self.ffiObject.decrementPointer()
	_uniffiRV, _uniffiErr := rustCallWithError[VeritasError](FfiConverterVeritasError{}, func(_uniffiStatus *C.RustCallStatus) RustBufferI {
		return GoRustBuffer{
			inner: C.uniffi_libveritas_uniffi_fn_method_veritaszone_to_json(
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
func (object *VeritasZone) Destroy() {
	runtime.SetFinalizer(object, nil)
	object.ffiObject.destroy()
}

type FfiConverterVeritasZone struct{}

var FfiConverterVeritasZoneINSTANCE = FfiConverterVeritasZone{}

func (c FfiConverterVeritasZone) Lift(pointer unsafe.Pointer) *VeritasZone {
	result := &VeritasZone{
		newFfiObject(
			pointer,
			func(pointer unsafe.Pointer, status *C.RustCallStatus) unsafe.Pointer {
				return C.uniffi_libveritas_uniffi_fn_clone_veritaszone(pointer, status)
			},
			func(pointer unsafe.Pointer, status *C.RustCallStatus) {
				C.uniffi_libveritas_uniffi_fn_free_veritaszone(pointer, status)
			},
		),
	}
	runtime.SetFinalizer(result, (*VeritasZone).Destroy)
	return result
}

func (c FfiConverterVeritasZone) Read(reader io.Reader) *VeritasZone {
	return c.Lift(unsafe.Pointer(uintptr(readUint64(reader))))
}

func (c FfiConverterVeritasZone) Lower(value *VeritasZone) unsafe.Pointer {
	// TODO: this is bad - all synchronization from ObjectRuntime.go is discarded here,
	// because the pointer will be decremented immediately after this function returns,
	// and someone will be left holding onto a non-locked pointer.
	pointer := value.ffiObject.incrementPointer("*VeritasZone")
	defer value.ffiObject.decrementPointer()
	return pointer

}

func (c FfiConverterVeritasZone) Write(writer io.Writer, value *VeritasZone) {
	writeUint64(writer, uint64(uintptr(c.Lower(value))))
}

type FfiDestroyerVeritasZone struct{}

func (_ FfiDestroyerVeritasZone) Destroy(value *VeritasZone) {
	value.Destroy()
}

type VeritasCertificate struct {
	Subject  string
	CertType string
	Bytes    []byte
}

func (r *VeritasCertificate) Destroy() {
	FfiDestroyerString{}.Destroy(r.Subject)
	FfiDestroyerString{}.Destroy(r.CertType)
	FfiDestroyerBytes{}.Destroy(r.Bytes)
}

type FfiConverterVeritasCertificate struct{}

var FfiConverterVeritasCertificateINSTANCE = FfiConverterVeritasCertificate{}

func (c FfiConverterVeritasCertificate) Lift(rb RustBufferI) VeritasCertificate {
	return LiftFromRustBuffer[VeritasCertificate](c, rb)
}

func (c FfiConverterVeritasCertificate) Read(reader io.Reader) VeritasCertificate {
	return VeritasCertificate{
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterStringINSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterVeritasCertificate) Lower(value VeritasCertificate) C.RustBuffer {
	return LowerIntoRustBuffer[VeritasCertificate](c, value)
}

func (c FfiConverterVeritasCertificate) Write(writer io.Writer, value VeritasCertificate) {
	FfiConverterStringINSTANCE.Write(writer, value.Subject)
	FfiConverterStringINSTANCE.Write(writer, value.CertType)
	FfiConverterBytesINSTANCE.Write(writer, value.Bytes)
}

type FfiDestroyerVeritasCertificate struct{}

func (_ FfiDestroyerVeritasCertificate) Destroy(value VeritasCertificate) {
	value.Destroy()
}

type VeritasOffchainData struct {
	Seq  uint32
	Data []byte
}

func (r *VeritasOffchainData) Destroy() {
	FfiDestroyerUint32{}.Destroy(r.Seq)
	FfiDestroyerBytes{}.Destroy(r.Data)
}

type FfiConverterVeritasOffchainData struct{}

var FfiConverterVeritasOffchainDataINSTANCE = FfiConverterVeritasOffchainData{}

func (c FfiConverterVeritasOffchainData) Lift(rb RustBufferI) VeritasOffchainData {
	return LiftFromRustBuffer[VeritasOffchainData](c, rb)
}

func (c FfiConverterVeritasOffchainData) Read(reader io.Reader) VeritasOffchainData {
	return VeritasOffchainData{
		FfiConverterUint32INSTANCE.Read(reader),
		FfiConverterBytesINSTANCE.Read(reader),
	}
}

func (c FfiConverterVeritasOffchainData) Lower(value VeritasOffchainData) C.RustBuffer {
	return LowerIntoRustBuffer[VeritasOffchainData](c, value)
}

func (c FfiConverterVeritasOffchainData) Write(writer io.Writer, value VeritasOffchainData) {
	FfiConverterUint32INSTANCE.Write(writer, value.Seq)
	FfiConverterBytesINSTANCE.Write(writer, value.Data)
}

type FfiDestroyerVeritasOffchainData struct{}

func (_ FfiDestroyerVeritasOffchainData) Destroy(value VeritasOffchainData) {
	value.Destroy()
}

type VeritasCommitmentState interface {
	Destroy()
}
type VeritasCommitmentStateExists struct {
	StateRoot   []byte
	PrevRoot    *[]byte
	RollingHash []byte
	BlockHeight uint32
	ReceiptHash *[]byte
}

func (e VeritasCommitmentStateExists) Destroy() {
	FfiDestroyerBytes{}.Destroy(e.StateRoot)
	FfiDestroyerOptionalBytes{}.Destroy(e.PrevRoot)
	FfiDestroyerBytes{}.Destroy(e.RollingHash)
	FfiDestroyerUint32{}.Destroy(e.BlockHeight)
	FfiDestroyerOptionalBytes{}.Destroy(e.ReceiptHash)
}

type VeritasCommitmentStateEmpty struct {
}

func (e VeritasCommitmentStateEmpty) Destroy() {
}

type VeritasCommitmentStateUnknown struct {
}

func (e VeritasCommitmentStateUnknown) Destroy() {
}

type FfiConverterVeritasCommitmentState struct{}

var FfiConverterVeritasCommitmentStateINSTANCE = FfiConverterVeritasCommitmentState{}

func (c FfiConverterVeritasCommitmentState) Lift(rb RustBufferI) VeritasCommitmentState {
	return LiftFromRustBuffer[VeritasCommitmentState](c, rb)
}

func (c FfiConverterVeritasCommitmentState) Lower(value VeritasCommitmentState) C.RustBuffer {
	return LowerIntoRustBuffer[VeritasCommitmentState](c, value)
}
func (FfiConverterVeritasCommitmentState) Read(reader io.Reader) VeritasCommitmentState {
	id := readInt32(reader)
	switch id {
	case 1:
		return VeritasCommitmentStateExists{
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterUint32INSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
		}
	case 2:
		return VeritasCommitmentStateEmpty{}
	case 3:
		return VeritasCommitmentStateUnknown{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterVeritasCommitmentState.Read()", id))
	}
}

func (FfiConverterVeritasCommitmentState) Write(writer io.Writer, value VeritasCommitmentState) {
	switch variant_value := value.(type) {
	case VeritasCommitmentStateExists:
		writeInt32(writer, 1)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.StateRoot)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.PrevRoot)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.RollingHash)
		FfiConverterUint32INSTANCE.Write(writer, variant_value.BlockHeight)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.ReceiptHash)
	case VeritasCommitmentStateEmpty:
		writeInt32(writer, 2)
	case VeritasCommitmentStateUnknown:
		writeInt32(writer, 3)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterVeritasCommitmentState.Write", value))
	}
}

type FfiDestroyerVeritasCommitmentState struct{}

func (_ FfiDestroyerVeritasCommitmentState) Destroy(value VeritasCommitmentState) {
	value.Destroy()
}

type VeritasDelegateState interface {
	Destroy()
}
type VeritasDelegateStateExists struct {
	ScriptPubkey []byte
	Data         *[]byte
	OffchainData *VeritasOffchainData
}

func (e VeritasDelegateStateExists) Destroy() {
	FfiDestroyerBytes{}.Destroy(e.ScriptPubkey)
	FfiDestroyerOptionalBytes{}.Destroy(e.Data)
	FfiDestroyerOptionalVeritasOffchainData{}.Destroy(e.OffchainData)
}

type VeritasDelegateStateEmpty struct {
}

func (e VeritasDelegateStateEmpty) Destroy() {
}

type VeritasDelegateStateUnknown struct {
}

func (e VeritasDelegateStateUnknown) Destroy() {
}

type FfiConverterVeritasDelegateState struct{}

var FfiConverterVeritasDelegateStateINSTANCE = FfiConverterVeritasDelegateState{}

func (c FfiConverterVeritasDelegateState) Lift(rb RustBufferI) VeritasDelegateState {
	return LiftFromRustBuffer[VeritasDelegateState](c, rb)
}

func (c FfiConverterVeritasDelegateState) Lower(value VeritasDelegateState) C.RustBuffer {
	return LowerIntoRustBuffer[VeritasDelegateState](c, value)
}
func (FfiConverterVeritasDelegateState) Read(reader io.Reader) VeritasDelegateState {
	id := readInt32(reader)
	switch id {
	case 1:
		return VeritasDelegateStateExists{
			FfiConverterBytesINSTANCE.Read(reader),
			FfiConverterOptionalBytesINSTANCE.Read(reader),
			FfiConverterOptionalVeritasOffchainDataINSTANCE.Read(reader),
		}
	case 2:
		return VeritasDelegateStateEmpty{}
	case 3:
		return VeritasDelegateStateUnknown{}
	default:
		panic(fmt.Sprintf("invalid enum value %v in FfiConverterVeritasDelegateState.Read()", id))
	}
}

func (FfiConverterVeritasDelegateState) Write(writer io.Writer, value VeritasDelegateState) {
	switch variant_value := value.(type) {
	case VeritasDelegateStateExists:
		writeInt32(writer, 1)
		FfiConverterBytesINSTANCE.Write(writer, variant_value.ScriptPubkey)
		FfiConverterOptionalBytesINSTANCE.Write(writer, variant_value.Data)
		FfiConverterOptionalVeritasOffchainDataINSTANCE.Write(writer, variant_value.OffchainData)
	case VeritasDelegateStateEmpty:
		writeInt32(writer, 2)
	case VeritasDelegateStateUnknown:
		writeInt32(writer, 3)
	default:
		_ = variant_value
		panic(fmt.Sprintf("invalid enum value `%v` in FfiConverterVeritasDelegateState.Write", value))
	}
}

type FfiDestroyerVeritasDelegateState struct{}

func (_ FfiDestroyerVeritasDelegateState) Destroy(value VeritasDelegateState) {
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

type FfiConverterOptionalVeritasCertificate struct{}

var FfiConverterOptionalVeritasCertificateINSTANCE = FfiConverterOptionalVeritasCertificate{}

func (c FfiConverterOptionalVeritasCertificate) Lift(rb RustBufferI) *VeritasCertificate {
	return LiftFromRustBuffer[*VeritasCertificate](c, rb)
}

func (_ FfiConverterOptionalVeritasCertificate) Read(reader io.Reader) *VeritasCertificate {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterVeritasCertificateINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalVeritasCertificate) Lower(value *VeritasCertificate) C.RustBuffer {
	return LowerIntoRustBuffer[*VeritasCertificate](c, value)
}

func (_ FfiConverterOptionalVeritasCertificate) Write(writer io.Writer, value *VeritasCertificate) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterVeritasCertificateINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalVeritasCertificate struct{}

func (_ FfiDestroyerOptionalVeritasCertificate) Destroy(value *VeritasCertificate) {
	if value != nil {
		FfiDestroyerVeritasCertificate{}.Destroy(*value)
	}
}

type FfiConverterOptionalVeritasOffchainData struct{}

var FfiConverterOptionalVeritasOffchainDataINSTANCE = FfiConverterOptionalVeritasOffchainData{}

func (c FfiConverterOptionalVeritasOffchainData) Lift(rb RustBufferI) *VeritasOffchainData {
	return LiftFromRustBuffer[*VeritasOffchainData](c, rb)
}

func (_ FfiConverterOptionalVeritasOffchainData) Read(reader io.Reader) *VeritasOffchainData {
	if readInt8(reader) == 0 {
		return nil
	}
	temp := FfiConverterVeritasOffchainDataINSTANCE.Read(reader)
	return &temp
}

func (c FfiConverterOptionalVeritasOffchainData) Lower(value *VeritasOffchainData) C.RustBuffer {
	return LowerIntoRustBuffer[*VeritasOffchainData](c, value)
}

func (_ FfiConverterOptionalVeritasOffchainData) Write(writer io.Writer, value *VeritasOffchainData) {
	if value == nil {
		writeInt8(writer, 0)
	} else {
		writeInt8(writer, 1)
		FfiConverterVeritasOffchainDataINSTANCE.Write(writer, *value)
	}
}

type FfiDestroyerOptionalVeritasOffchainData struct{}

func (_ FfiDestroyerOptionalVeritasOffchainData) Destroy(value *VeritasOffchainData) {
	if value != nil {
		FfiDestroyerVeritasOffchainData{}.Destroy(*value)
	}
}

type FfiConverterSequenceVeritasZone struct{}

var FfiConverterSequenceVeritasZoneINSTANCE = FfiConverterSequenceVeritasZone{}

func (c FfiConverterSequenceVeritasZone) Lift(rb RustBufferI) []*VeritasZone {
	return LiftFromRustBuffer[[]*VeritasZone](c, rb)
}

func (c FfiConverterSequenceVeritasZone) Read(reader io.Reader) []*VeritasZone {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]*VeritasZone, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterVeritasZoneINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceVeritasZone) Lower(value []*VeritasZone) C.RustBuffer {
	return LowerIntoRustBuffer[[]*VeritasZone](c, value)
}

func (c FfiConverterSequenceVeritasZone) Write(writer io.Writer, value []*VeritasZone) {
	if len(value) > math.MaxInt32 {
		panic("[]*VeritasZone is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterVeritasZoneINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceVeritasZone struct{}

func (FfiDestroyerSequenceVeritasZone) Destroy(sequence []*VeritasZone) {
	for _, value := range sequence {
		FfiDestroyerVeritasZone{}.Destroy(value)
	}
}

type FfiConverterSequenceVeritasCertificate struct{}

var FfiConverterSequenceVeritasCertificateINSTANCE = FfiConverterSequenceVeritasCertificate{}

func (c FfiConverterSequenceVeritasCertificate) Lift(rb RustBufferI) []VeritasCertificate {
	return LiftFromRustBuffer[[]VeritasCertificate](c, rb)
}

func (c FfiConverterSequenceVeritasCertificate) Read(reader io.Reader) []VeritasCertificate {
	length := readInt32(reader)
	if length == 0 {
		return nil
	}
	result := make([]VeritasCertificate, 0, length)
	for i := int32(0); i < length; i++ {
		result = append(result, FfiConverterVeritasCertificateINSTANCE.Read(reader))
	}
	return result
}

func (c FfiConverterSequenceVeritasCertificate) Lower(value []VeritasCertificate) C.RustBuffer {
	return LowerIntoRustBuffer[[]VeritasCertificate](c, value)
}

func (c FfiConverterSequenceVeritasCertificate) Write(writer io.Writer, value []VeritasCertificate) {
	if len(value) > math.MaxInt32 {
		panic("[]VeritasCertificate is too large to fit into Int32")
	}

	writeInt32(writer, int32(len(value)))
	for _, item := range value {
		FfiConverterVeritasCertificateINSTANCE.Write(writer, item)
	}
}

type FfiDestroyerSequenceVeritasCertificate struct{}

func (FfiDestroyerSequenceVeritasCertificate) Destroy(sequence []VeritasCertificate) {
	for _, value := range sequence {
		FfiDestroyerVeritasCertificate{}.Destroy(value)
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
