// P3-M9 (real-reflect, Phase 3): synthetic `internal/cpu` package.
//
// REAL Go source (hand-written against the gc 1.27.1 API surface),
// embedded as text and type-checked on demand with the overlay importer
// (see synth_bytealg.go for the pattern). Synthetic-only, like
// internal/reflectlite and internal/bytealg: user programs cannot import
// internal/* (the import allow-list does not admit it), but packages in
// the real reflect dependency closure (internal/runtime/maps) may —
// resolved via overlayImporter.Import("internal/cpu").
//
// API: the gc 1.27.1 exported surface — CacheLinePad, CacheLinePadSize,
// CacheLineSize, the per-arch feature structs (X86, ARM, ARM64, Loong64,
// MIPS64X, PPC64, S390X, RISCV64) with identical field names, and
// Initialize. The feature DETECTION (cpuid/xgetbv assembly, per-arch
// doinit) is platform code with no portable equivalent: the graalgo
// backend has no CPUID-gated assembly consumers, so every feature bit
// stays false and every consumer takes its portable path (e.g.
// internal/runtime/maps AlgInit leaves UseAeshash false and uses the
// portable hash). This is a platform adaptation, not a logic duplication:
// there is no Go logic to duplicate — the detection is assembly.
//
// Deviations from gc (documented, none observable to correct programs):
//
//	D1. All feature bits are false (portable paths everywhere). A program
//	    observing a feature bit would see environment truth under gc and
//	    false here; every in-tree consumer uses the bits only to select
//	    between behaviorally-identical fast/slow paths.
//	D2. Initialize is a no-op (no GODEBUG cpu options processing).
//	D3. The //go:linkname markers on the feature vars (for assembly
//	    consumers) are omitted: there are no assembly consumers.
//	D4. CacheLinePadSize = 64: the amd64 value (the backend targets
//	    amd64; types.SizesFor("gc", "amd64") is the layout everywhere).
package main

// syntheticCpuSource is the synthetic `internal/cpu` package: exact gc
// 1.27.1 exported API; all feature detection reports absent.
const syntheticCpuSource = `package cpu

// CacheLinePadSize is the CPU's assumed cache line size (amd64 value).
const CacheLinePadSize = 64

// CacheLinePad is used to pad structs to avoid false sharing.
type CacheLinePad struct{ _ [CacheLinePadSize]byte }

// CacheLineSize is the CPU's assumed cache line size.
var CacheLineSize uintptr = CacheLinePadSize

// The booleans in X86 contain the correspondingly named cpuid feature bit.
// The struct is padded to avoid false sharing.
var X86 struct {
	_                   CacheLinePad
	HasAES              bool
	HasADX              bool
	HasAVX              bool
	HasAVXVNNI          bool
	HasAVX2             bool
	HasAVX512           bool // Virtual feature: F+CD+BW+DQ+VL
	HasAVX512F          bool
	HasAVX512CD         bool
	HasAVX512BW         bool
	HasAVX512DQ         bool
	HasAVX512VL         bool
	HasAVX512GFNI       bool
	HasAVX512VAES       bool
	HasAVX512VNNI       bool
	HasAVX512VBMI       bool
	HasAVX512VBMI2      bool
	HasAVX512BITALG     bool
	HasAVX512VPOPCNTDQ  bool
	HasAVX512VPCLMULQDQ bool
	HasBMI1             bool
	HasBMI2             bool
	HasERMS             bool
	HasFSRM             bool
	HasFMA              bool
	HasGFNI             bool
	HasOSXSAVE          bool
	HasPCLMULQDQ        bool
	HasPOPCNT           bool
	HasRDTSCP           bool
	HasSHA              bool
	HasSSE3             bool
	HasSSSE3            bool
	HasSSE41            bool
	HasSSE42            bool
	HasVAES             bool
	HasVPCLMULQDQ       bool
	_                   CacheLinePad
}

// The booleans in ARM contain the correspondingly named cpu feature bit.
// The struct is padded to avoid false sharing.
var ARM struct {
	_            CacheLinePad
	HasVFPv4     bool
	HasIDIVA     bool
	HasV7Atomics bool
	_            CacheLinePad
}

// The booleans in ARM64 contain the correspondingly named cpu feature bit.
// The struct is padded to avoid false sharing.
var ARM64 struct {
	_          CacheLinePad
	HasAES     bool
	HasPMULL   bool
	HasSHA1    bool
	HasSHA2    bool
	HasSHA512  bool
	HasSHA3    bool
	HasCRC32   bool
	HasATOMICS bool
	HasCPUID   bool
	HasDIT     bool
	HasSB      bool
	IsNeoverse bool
	_          CacheLinePad
}

// The booleans in Loong64 contain the correspondingly named cpu feature bit.
// The struct is padded to avoid false sharing.
var Loong64 struct {
	_              CacheLinePad
	HasLSX         bool // support 128-bit vector extension
	HasLASX        bool // support 256-bit vector extension
	HasCRC32       bool // support CRC instruction
	HasLAMCAS      bool // support AMCAS[_DB].{B/H/W/D}
	HasLAM_BH      bool // support AM{SWAP/ADD}[_DB].{B/H} instruction
	HasLLACQ_SCREL bool // support LLACQ.{W/D}, SCREL.{W/D} instruction
	HasSCQ         bool // support SC.Q instruction
	HasDBAR_HINTS  bool // supports finer-grained DBAR hints
	_              CacheLinePad
}

var MIPS64X struct {
	_      CacheLinePad
	HasMSA bool // MIPS SIMD architecture
	_      CacheLinePad
}

// For ppc64(le), it is safe to check only for ISA level starting on ISA v3.00,
// since there are no optional categories. There are some exceptions that also
// require kernel support to work (darn, scv), so there are feature bits for
// those as well. The minimum processor requirement is POWER8 (ISA 2.07).
// The struct is padded to avoid false sharing.
var PPC64 struct {
	_         CacheLinePad
	HasDARN   bool // Hardware random number generator (requires kernel enablement)
	HasSCV    bool // Syscall vectored (requires kernel enablement)
	IsPOWER8  bool // ISA v2.07 (POWER8)
	IsPOWER9  bool // ISA v3.00 (POWER9)
	IsPOWER10 bool // ISA v3.1  (POWER10)
	_         CacheLinePad
}

var S390X struct {
	_         CacheLinePad
	HasZARCH  bool // z architecture mode is active [mandatory]
	HasSTFLE  bool // store facility list extended [mandatory]
	HasLDISP  bool // long (20-bit) displacements [mandatory]
	HasEIMM   bool // 32-bit immediates [mandatory]
	HasDFP    bool // decimal floating point
	HasETF3EH bool // ETF-3 enhanced
	HasMSA    bool // message security assist (CPACF)
	HasAES    bool // KM-AES{128,192,256} functions
	HasAESCBC bool // KMC-AES{128,192,256} functions
	HasAESCTR bool // KMCTR-AES{128,192,256} functions
	HasAESGCM bool // KMA-GCM-AES{128,192,256} functions
	HasGHASH  bool // KIMD-GHASH function
	HasSHA1   bool // K{I,L}MD-SHA-1 functions
	HasSHA256 bool // K{I,L}MD-SHA-256 functions
	HasSHA512 bool // K{I,L}MD-SHA-512 functions
	HasSHA3   bool // K{I,L}MD-SHA3-{224,256,384,512} and K{I,L}MD-SHAKE-{128,256} functions
	HasVX     bool // vector facility. Note: the runtime sets this when it processes auxv records.
	HasVXE    bool // vector-enhancements facility 1
	HasKDSA   bool // elliptic curve functions
	HasECDSA  bool // NIST curves
	HasEDDSA  bool // Edwards curves
	_         CacheLinePad
}

// RISCV64 contains the supported CPU features and performance characteristics for riscv64
// platforms. The booleans in RISCV64, with the exception of HasFastMisaligned, indicate
// the presence of RISC-V extensions.
var RISCV64 struct {
	_                 CacheLinePad
	HasFastMisaligned bool // Fast misaligned accesses
	HasV              bool // Vector extension compatible with RVV 1.0
	HasZbb            bool // Basic bit-manipulation extension
	HasZbc            bool // Carryless multiplication extension
	HasZvbb           bool // Vector Basic Bit-manipulation
	HasZvbc           bool // Vector Carryless Multiplication
	HasZvkg           bool // Vector GCM/GMAC
	HasZvkned         bool // NIST Suite: Vector AES Block Cipher
	HasZvknha         bool // NIST Suite: Vector SHA-2 Secure Hash
	HasZvknhb         bool // NIST Suite: Vector SHA-2 Secure Hash
	HasZvksed         bool // ShangMi Suite: SM4 Block Cipher
	HasZvksh          bool // ShangMi Suite: SM3 Secure Hash
	HasZvkt           bool // Vector Data-Independent Execution Latency
	_                 CacheLinePad
}

// doDerived, if non-nil, is called after processing GODEBUG to set "derived"
// feature flags.
var doDerived func()

// Initialize examines the processor and sets the relevant variables above.
// On the graalgo backend there is no CPUID-gated assembly, so this is a
// no-op: every feature bit stays false and consumers take portable paths.
func Initialize(env string) {}
`
