// https://github.com/tinygo-org/tinygo/blob/2a76ceb7dd5ea5a834ec470b724882564d9681b3/src/runtime/arch_tinygowasm.go#L42
// https://github.com/tinygo-org/tinygo/blob/2a76ceb7dd5ea5a834ec470b724882564d9681b3/src/runtime/gc_leaking.go

//go:build gc.custom

package runtime

// This GC implementation is the simplest useful memory allocator possible: it
// only allocates memory and never frees it. For some constrained systems, it
// may be the only memory allocator possible.

import (
	realRuntime "runtime"
	"unsafe"
)

// https://github.com/tinygo-org/tinygo/blob/2a76ceb7dd5ea5a834ec470b724882564d9681b3/src/runtime/arch_tinygowasm.go#L19
//
//go:extern __heap_base
var heapStartSymbol [0]byte

var heapStart = uintptr(unsafe.Pointer(&heapStartSymbol))

// https://github.com/tinygo-org/tinygo/blob/2a76ceb7dd5ea5a834ec470b724882564d9681b3/src/runtime/arch_tinygowasm.go#L34
//
//export llvm.wasm.memory.size.i32
func wasm_memory_size(index int32) int32

// https://github.com/tinygo-org/tinygo/blob/2a76ceb7dd5ea5a834ec470b724882564d9681b3/src/runtime/arch_tinygowasm.go#L34
const wasmPageSize = 64 * 1024

var heapEnd = uintptr(wasm_memory_size(0) * wasmPageSize)

// Ever-incrementing pointer: no memory is freed.
var heapptr = heapStart

// Total amount allocated for runtime.MemStats
var gcTotalAlloc uint64

// Total number of calls to alloc()
var gcMallocs uint64

// Total number of objected freed; for leaking collector this stays 0
const gcFrees = 0

// trap is a compiler hint that this function cannot be executed. It is
// translated into either a trap instruction or a call to abort().
//
//export llvm.trap
func trap()

//go:wasmexport alloc
func Alloc(size uintptr) unsafe.Pointer {
	return alloc(size, unsafe.Pointer(nil))
}

// Inlining alloc() speeds things up slightly but bloats the executable by 50%,
// see https://github.com/tinygo-org/tinygo/issues/2674.  So don't.
//
//go:noinline
//go:linkname alloc runtime.alloc
func alloc(size uintptr, layout unsafe.Pointer) unsafe.Pointer {
	size = align(size)
	addr := heapptr
	gcTotalAlloc += uint64(size)
	gcMallocs++
	heapptr += size
	for heapptr >= heapEnd {
		if growHeap() {
			continue
		}
		trap()
	}
	pointer := unsafe.Pointer(addr)
	zero_new_alloc(pointer, size)
	return pointer
}

//go:inline
func zero_new_alloc(ptr unsafe.Pointer, size uintptr) {
	memzero(ptr, size)
}

//go:linkname memcpy runtime.memcpy
func memcpy(dst, src unsafe.Pointer, size uintptr)

//go:linkname memmove runtime.memmove
func memmove(dst, src unsafe.Pointer, size uintptr)

//go:linkname memzero runtime.memzero
func memzero(ptr unsafe.Pointer, size uintptr)

//go:linkname realloc runtime.realloc
func realloc(ptr unsafe.Pointer, size uintptr) unsafe.Pointer {
	newAlloc := alloc(size, nil)
	if ptr == nil {
		return newAlloc
	}
	memcpy(newAlloc, ptr, size)
	return newAlloc
}

//go:linkname free runtime.free
func free(ptr unsafe.Pointer) {
	// Memory is never freed.
}

//go:linkname ReadMemStats runtime.ReadMemStats
func ReadMemStats(m *realRuntime.MemStats) {
	m.HeapIdle = 0
	m.HeapInuse = gcTotalAlloc
	m.HeapReleased = 0
	m.HeapSys = m.HeapInuse + m.HeapIdle
	m.GCSys = 0
	m.TotalAlloc = gcTotalAlloc
	m.Mallocs = gcMallocs
	m.Frees = gcFrees
	m.Sys = uint64(heapEnd - heapStart)
	m.HeapAlloc = gcTotalAlloc
	m.Alloc = m.HeapAlloc
}

//go:linkname GC runtime.GC
func GC() {
	// No-op.
}

func SetFinalizer(obj interface{}, finalizer interface{}) {
	// No-op.
}

//go:linkname initHeap runtime.initHeap
func initHeap() {
	heapptr = heapStart
}

const wasmMemoryIndex = 0

//export llvm.wasm.memory.grow.i32
func wasm_memory_grow(index int32, delta int32) int32

func growHeap() bool {
	memorySize := wasm_memory_size(wasmMemoryIndex)
	result := wasm_memory_grow(wasmMemoryIndex, memorySize)
	if result == -1 {
		return false
	}
	heapEnd = uintptr(wasm_memory_size(wasmMemoryIndex) * wasmPageSize)
	return true
}

func align(ptr uintptr) uintptr {
	const heapAlign = 16
	return (ptr + heapAlign - 1) &^ (heapAlign - 1)
}
