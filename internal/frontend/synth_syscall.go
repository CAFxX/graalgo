// P3-M8 (wave 2): synthetic `syscall` package (linux/amd64).
//
// REAL Go source, embedded as text and type-checked on demand with the
// overlay importer. Per R1/R2/R3: vocabulary + scalar services ONLY.
//   - IN: errno/SYS_*/O_*/S_*/DT_* constants (copied verbatim from
//     zerrors_linux_amd64.go / zsysnum_linux_amd64.go), struct types
//     (verbatim from ztypes_linux_amd64.go), Errno/Signal/WaitStatus
//   - methods, errnoErr, the env family, entersyscall/exitsyscall
//     no-ops, and scalar wrappers (Getenv/Getpid/Getuid/Exit/...)
//     binding the FROZEN graalgo.os.* table (os.md §2.4) via
//     //go:linkname (ADR-0004). The os worker implements the Java
//     side in GoOs.java; one Linknames registration serves both
//     synthetic packages.
//   - FAIL-FAST with exact trap-style messages naming re-test
//     triggers: Syscall/RawSyscall/Syscall6/RawSyscall6 (arbitrary
//     traps), the I/O wrappers (Open/Read/Write/Close/Stat/Mkdir/...),
//     StartProcess/ForkExec/Wait4, sockets/netlink, Mmap, Flock/
//     rlimit/setuid/epoll/inotify. The zsyscall wrappers are NOT reused
//     verbatim (they funnel untyped uintptrs through Syscall);
//     everything is re-authored as typed Go.
//
// Multi-result members keep exact gc signatures with fail-fast bodies;
// the real implementations (Getenv/Getgroups/Getwd) and the fail-fast
// helpers live in single-result __-helpers returning pair structs, and
// 2/3-target call sites desugar via Appendix M (staged mrdesugar_m5.go
// entries). Every other position fails fast (WS D).
package main

// syntheticSyscallSource is the P3-M8 synthetic `syscall` package.
const syntheticSyscallSource = `package syscall

import (
	errorspkg "errors"
	"io/fs"
	"strconv"

	"unsafe"
)

// An Errno is an unsigned number describing an error condition.
// It implements the error interface. The zero Errno is by convention
// a non-error, so code to convert from Errno to error should use:
//
//	err = nil
//	if errno != 0 {
//		err = errno
//	}
//
// Errno values can be tested against error values using [errors.Is].
type Errno uintptr

func (e Errno) Error() string {
	if 0 <= int(e) && int(e) < len(errors) {
		s := errors[e]
		if s != "" {
			return s
		}
	}
	return "errno " + strconv.Itoa(int(e))
}

// Is reports whether e matches target. The mapping is gc's exact
// linux/amd64 mapping (syscall_unix.go): the fs.* sentinels are the
// synthetic io/fs package's (shared instance with synthetic os, so
// errors.Is(err, fs.ErrNotExist) and os.IsNotExist work across the
// package boundary), errors.ErrUnsupported is the synthetic errors
// package's.
func (e Errno) Is(target error) bool {
	switch target {
	case fs.ErrPermission:
		return e == EACCES || e == EPERM
	case fs.ErrExist:
		return e == EEXIST || e == ENOTEMPTY
	case fs.ErrNotExist:
		return e == ENOENT
	case errorspkg.ErrUnsupported:
		return e == ENOSYS || e == ENOTSUP || e == EOPNOTSUPP
	}
	return false
}

func (e Errno) Temporary() bool {
	return e == EINTR || e == EMFILE || e == ENFILE || e.Timeout()
}

func (e Errno) Timeout() bool {
	return e == EAGAIN || e == EWOULDBLOCK || e == ETIMEDOUT
}

// Do the interface allocations only once for common
// Errno values.
var (
	errEAGAIN error = EAGAIN
	errEINVAL error = EINVAL
	errENOENT error = ENOENT
)

// errnoErr returns common boxed Errno values, to prevent
// allocations at runtime.
func errnoErr(e Errno) error {
	switch e {
	case 0:
		return nil
	case EAGAIN:
		return errEAGAIN
	case EINVAL:
		return errEINVAL
	case ENOENT:
		return errENOENT
	}
	return e
}

// A Signal is a number describing a process signal.
// It implements the [os.Signal] interface.
type Signal int

func (s Signal) Signal() {}

func (s Signal) String() string {
	if 0 <= s && int(s) < len(signals) {
		str := signals[s]
		if str != "" {
			return str
		}
	}
	return "signal " + strconv.Itoa(int(s))
}

var (
	Stdin  = 0
	Stdout = 1
	Stderr = 2
)

const ImplementsGetwd = true

const (
	AF_ALG                           = 0x26
	AF_APPLETALK                     = 0x5
	AF_ASH                           = 0x12
	AF_ATMPVC                        = 0x8
	AF_ATMSVC                        = 0x14
	AF_AX25                          = 0x3
	AF_BLUETOOTH                     = 0x1f
	AF_BRIDGE                        = 0x7
	AF_CAIF                          = 0x25
	AF_CAN                           = 0x1d
	AF_DECnet                        = 0xc
	AF_ECONET                        = 0x13
	AF_FILE                          = 0x1
	AF_IEEE802154                    = 0x24
	AF_INET                          = 0x2
	AF_INET6                         = 0xa
	AF_IPX                           = 0x4
	AF_IRDA                          = 0x17
	AF_ISDN                          = 0x22
	AF_IUCV                          = 0x20
	AF_KEY                           = 0xf
	AF_LLC                           = 0x1a
	AF_LOCAL                         = 0x1
	AF_MAX                           = 0x27
	AF_NETBEUI                       = 0xd
	AF_NETLINK                       = 0x10
	AF_NETROM                        = 0x6
	AF_PACKET                        = 0x11
	AF_PHONET                        = 0x23
	AF_PPPOX                         = 0x18
	AF_RDS                           = 0x15
	AF_ROSE                          = 0xb
	AF_ROUTE                         = 0x10
	AF_RXRPC                         = 0x21
	AF_SECURITY                      = 0xe
	AF_SNA                           = 0x16
	AF_TIPC                          = 0x1e
	AF_UNIX                          = 0x1
	AF_UNSPEC                        = 0x0
	AF_WANPIPE                       = 0x19
	AF_X25                           = 0x9
	ARPHRD_ADAPT                     = 0x108
	ARPHRD_APPLETLK                  = 0x8
	ARPHRD_ARCNET                    = 0x7
	ARPHRD_ASH                       = 0x30d
	ARPHRD_ATM                       = 0x13
	ARPHRD_AX25                      = 0x3
	ARPHRD_BIF                       = 0x307
	ARPHRD_CHAOS                     = 0x5
	ARPHRD_CISCO                     = 0x201
	ARPHRD_CSLIP                     = 0x101
	ARPHRD_CSLIP6                    = 0x103
	ARPHRD_DDCMP                     = 0x205
	ARPHRD_DLCI                      = 0xf
	ARPHRD_ECONET                    = 0x30e
	ARPHRD_EETHER                    = 0x2
	ARPHRD_ETHER                     = 0x1
	ARPHRD_EUI64                     = 0x1b
	ARPHRD_FCAL                      = 0x311
	ARPHRD_FCFABRIC                  = 0x313
	ARPHRD_FCPL                      = 0x312
	ARPHRD_FCPP                      = 0x310
	ARPHRD_FDDI                      = 0x306
	ARPHRD_FRAD                      = 0x302
	ARPHRD_HDLC                      = 0x201
	ARPHRD_HIPPI                     = 0x30c
	ARPHRD_HWX25                     = 0x110
	ARPHRD_IEEE1394                  = 0x18
	ARPHRD_IEEE802                   = 0x6
	ARPHRD_IEEE80211                 = 0x321
	ARPHRD_IEEE80211_PRISM           = 0x322
	ARPHRD_IEEE80211_RADIOTAP        = 0x323
	ARPHRD_IEEE802154                = 0x324
	ARPHRD_IEEE802154_PHY            = 0x325
	ARPHRD_IEEE802_TR                = 0x320
	ARPHRD_INFINIBAND                = 0x20
	ARPHRD_IPDDP                     = 0x309
	ARPHRD_IPGRE                     = 0x30a
	ARPHRD_IRDA                      = 0x30f
	ARPHRD_LAPB                      = 0x204
	ARPHRD_LOCALTLK                  = 0x305
	ARPHRD_LOOPBACK                  = 0x304
	ARPHRD_METRICOM                  = 0x17
	ARPHRD_NETROM                    = 0x0
	ARPHRD_NONE                      = 0xfffe
	ARPHRD_PIMREG                    = 0x30b
	ARPHRD_PPP                       = 0x200
	ARPHRD_PRONET                    = 0x4
	ARPHRD_RAWHDLC                   = 0x206
	ARPHRD_ROSE                      = 0x10e
	ARPHRD_RSRVD                     = 0x104
	ARPHRD_SIT                       = 0x308
	ARPHRD_SKIP                      = 0x303
	ARPHRD_SLIP                      = 0x100
	ARPHRD_SLIP6                     = 0x102
	ARPHRD_TUNNEL                    = 0x300
	ARPHRD_TUNNEL6                   = 0x301
	ARPHRD_VOID                      = 0xffff
	ARPHRD_X25                       = 0x10f
	BPF_A                            = 0x10
	BPF_ABS                          = 0x20
	BPF_ADD                          = 0x0
	BPF_ALU                          = 0x4
	BPF_AND                          = 0x50
	BPF_B                            = 0x10
	BPF_DIV                          = 0x30
	BPF_H                            = 0x8
	BPF_IMM                          = 0x0
	BPF_IND                          = 0x40
	BPF_JA                           = 0x0
	BPF_JEQ                          = 0x10
	BPF_JGE                          = 0x30
	BPF_JGT                          = 0x20
	BPF_JMP                          = 0x5
	BPF_JSET                         = 0x40
	BPF_K                            = 0x0
	BPF_LD                           = 0x0
	BPF_LDX                          = 0x1
	BPF_LEN                          = 0x80
	BPF_LSH                          = 0x60
	BPF_MAJOR_VERSION                = 0x1
	BPF_MAXINSNS                     = 0x1000
	BPF_MEM                          = 0x60
	BPF_MEMWORDS                     = 0x10
	BPF_MINOR_VERSION                = 0x1
	BPF_MISC                         = 0x7
	BPF_MSH                          = 0xa0
	BPF_MUL                          = 0x20
	BPF_NEG                          = 0x80
	BPF_OR                           = 0x40
	BPF_RET                          = 0x6
	BPF_RSH                          = 0x70
	BPF_ST                           = 0x2
	BPF_STX                          = 0x3
	BPF_SUB                          = 0x10
	BPF_TAX                          = 0x0
	BPF_TXA                          = 0x80
	BPF_W                            = 0x0
	BPF_X                            = 0x8
	DT_BLK                           = 0x6
	DT_CHR                           = 0x2
	DT_DIR                           = 0x4
	DT_FIFO                          = 0x1
	DT_LNK                           = 0xa
	DT_REG                           = 0x8
	DT_SOCK                          = 0xc
	DT_UNKNOWN                       = 0x0
	DT_WHT                           = 0xe
	EPOLLERR                         = 0x8
	EPOLLET                          = -0x80000000
	EPOLLHUP                         = 0x10
	EPOLLIN                          = 0x1
	EPOLLMSG                         = 0x400
	EPOLLONESHOT                     = 0x40000000
	EPOLLOUT                         = 0x4
	EPOLLPRI                         = 0x2
	EPOLLRDBAND                      = 0x80
	EPOLLRDHUP                       = 0x2000
	EPOLLRDNORM                      = 0x40
	EPOLLWRBAND                      = 0x200
	EPOLLWRNORM                      = 0x100
	EPOLL_CLOEXEC                    = 0x80000
	EPOLL_CTL_ADD                    = 0x1
	EPOLL_CTL_DEL                    = 0x2
	EPOLL_CTL_MOD                    = 0x3
	EPOLL_NONBLOCK                   = 0x800
	ETH_P_1588                       = 0x88f7
	ETH_P_8021Q                      = 0x8100
	ETH_P_802_2                      = 0x4
	ETH_P_802_3                      = 0x1
	ETH_P_AARP                       = 0x80f3
	ETH_P_ALL                        = 0x3
	ETH_P_AOE                        = 0x88a2
	ETH_P_ARCNET                     = 0x1a
	ETH_P_ARP                        = 0x806
	ETH_P_ATALK                      = 0x809b
	ETH_P_ATMFATE                    = 0x8884
	ETH_P_ATMMPOA                    = 0x884c
	ETH_P_AX25                       = 0x2
	ETH_P_BPQ                        = 0x8ff
	ETH_P_CAIF                       = 0xf7
	ETH_P_CAN                        = 0xc
	ETH_P_CONTROL                    = 0x16
	ETH_P_CUST                       = 0x6006
	ETH_P_DDCMP                      = 0x6
	ETH_P_DEC                        = 0x6000
	ETH_P_DIAG                       = 0x6005
	ETH_P_DNA_DL                     = 0x6001
	ETH_P_DNA_RC                     = 0x6002
	ETH_P_DNA_RT                     = 0x6003
	ETH_P_DSA                        = 0x1b
	ETH_P_ECONET                     = 0x18
	ETH_P_EDSA                       = 0xdada
	ETH_P_FCOE                       = 0x8906
	ETH_P_FIP                        = 0x8914
	ETH_P_HDLC                       = 0x19
	ETH_P_IEEE802154                 = 0xf6
	ETH_P_IEEEPUP                    = 0xa00
	ETH_P_IEEEPUPAT                  = 0xa01
	ETH_P_IP                         = 0x800
	ETH_P_IPV6                       = 0x86dd
	ETH_P_IPX                        = 0x8137
	ETH_P_IRDA                       = 0x17
	ETH_P_LAT                        = 0x6004
	ETH_P_LINK_CTL                   = 0x886c
	ETH_P_LOCALTALK                  = 0x9
	ETH_P_LOOP                       = 0x60
	ETH_P_MOBITEX                    = 0x15
	ETH_P_MPLS_MC                    = 0x8848
	ETH_P_MPLS_UC                    = 0x8847
	ETH_P_PAE                        = 0x888e
	ETH_P_PAUSE                      = 0x8808
	ETH_P_PHONET                     = 0xf5
	ETH_P_PPPTALK                    = 0x10
	ETH_P_PPP_DISC                   = 0x8863
	ETH_P_PPP_MP                     = 0x8
	ETH_P_PPP_SES                    = 0x8864
	ETH_P_PUP                        = 0x200
	ETH_P_PUPAT                      = 0x201
	ETH_P_RARP                       = 0x8035
	ETH_P_SCA                        = 0x6007
	ETH_P_SLOW                       = 0x8809
	ETH_P_SNAP                       = 0x5
	ETH_P_TEB                        = 0x6558
	ETH_P_TIPC                       = 0x88ca
	ETH_P_TRAILER                    = 0x1c
	ETH_P_TR_802_2                   = 0x11
	ETH_P_WAN_PPP                    = 0x7
	ETH_P_WCCP                       = 0x883e
	ETH_P_X25                        = 0x805
	FD_CLOEXEC                       = 0x1
	FD_SETSIZE                       = 0x400
	F_DUPFD                          = 0x0
	F_DUPFD_CLOEXEC                  = 0x406
	F_EXLCK                          = 0x4
	F_GETFD                          = 0x1
	F_GETFL                          = 0x3
	F_GETLEASE                       = 0x401
	F_GETLK                          = 0x5
	F_GETLK64                        = 0x5
	F_GETOWN                         = 0x9
	F_GETOWN_EX                      = 0x10
	F_GETPIPE_SZ                     = 0x408
	F_GETSIG                         = 0xb
	F_LOCK                           = 0x1
	F_NOTIFY                         = 0x402
	F_OK                             = 0x0
	F_RDLCK                          = 0x0
	F_SETFD                          = 0x2
	F_SETFL                          = 0x4
	F_SETLEASE                       = 0x400
	F_SETLK                          = 0x6
	F_SETLK64                        = 0x6
	F_SETLKW                         = 0x7
	F_SETLKW64                       = 0x7
	F_SETOWN                         = 0x8
	F_SETOWN_EX                      = 0xf
	F_SETPIPE_SZ                     = 0x407
	F_SETSIG                         = 0xa
	F_SHLCK                          = 0x8
	F_TEST                           = 0x3
	F_TLOCK                          = 0x2
	F_ULOCK                          = 0x0
	F_UNLCK                          = 0x2
	F_WRLCK                          = 0x1
	ICMPV6_FILTER                    = 0x1
	IFA_F_DADFAILED                  = 0x8
	IFA_F_DEPRECATED                 = 0x20
	IFA_F_HOMEADDRESS                = 0x10
	IFA_F_NODAD                      = 0x2
	IFA_F_OPTIMISTIC                 = 0x4
	IFA_F_PERMANENT                  = 0x80
	IFA_F_SECONDARY                  = 0x1
	IFA_F_TEMPORARY                  = 0x1
	IFA_F_TENTATIVE                  = 0x40
	IFA_MAX                          = 0x7
	IFF_ALLMULTI                     = 0x200
	IFF_AUTOMEDIA                    = 0x4000
	IFF_BROADCAST                    = 0x2
	IFF_DEBUG                        = 0x4
	IFF_DYNAMIC                      = 0x8000
	IFF_LOOPBACK                     = 0x8
	IFF_MASTER                       = 0x400
	IFF_MULTICAST                    = 0x1000
	IFF_NOARP                        = 0x80
	IFF_NOTRAILERS                   = 0x20
	IFF_NO_PI                        = 0x1000
	IFF_ONE_QUEUE                    = 0x2000
	IFF_POINTOPOINT                  = 0x10
	IFF_PORTSEL                      = 0x2000
	IFF_PROMISC                      = 0x100
	IFF_RUNNING                      = 0x40
	IFF_SLAVE                        = 0x800
	IFF_TAP                          = 0x2
	IFF_TUN                          = 0x1
	IFF_TUN_EXCL                     = 0x8000
	IFF_UP                           = 0x1
	IFF_VNET_HDR                     = 0x4000
	IFNAMSIZ                         = 0x10
	IN_ACCESS                        = 0x1
	IN_ALL_EVENTS                    = 0xfff
	IN_ATTRIB                        = 0x4
	IN_CLASSA_HOST                   = 0xffffff
	IN_CLASSA_MAX                    = 0x80
	IN_CLASSA_NET                    = 0xff000000
	IN_CLASSA_NSHIFT                 = 0x18
	IN_CLASSB_HOST                   = 0xffff
	IN_CLASSB_MAX                    = 0x10000
	IN_CLASSB_NET                    = 0xffff0000
	IN_CLASSB_NSHIFT                 = 0x10
	IN_CLASSC_HOST                   = 0xff
	IN_CLASSC_NET                    = 0xffffff00
	IN_CLASSC_NSHIFT                 = 0x8
	IN_CLOEXEC                       = 0x80000
	IN_CLOSE                         = 0x18
	IN_CLOSE_NOWRITE                 = 0x10
	IN_CLOSE_WRITE                   = 0x8
	IN_CREATE                        = 0x100
	IN_DELETE                        = 0x200
	IN_DELETE_SELF                   = 0x400
	IN_DONT_FOLLOW                   = 0x2000000
	IN_EXCL_UNLINK                   = 0x4000000
	IN_IGNORED                       = 0x8000
	IN_ISDIR                         = 0x40000000
	IN_LOOPBACKNET                   = 0x7f
	IN_MASK_ADD                      = 0x20000000
	IN_MODIFY                        = 0x2
	IN_MOVE                          = 0xc0
	IN_MOVED_FROM                    = 0x40
	IN_MOVED_TO                      = 0x80
	IN_MOVE_SELF                     = 0x800
	IN_NONBLOCK                      = 0x800
	IN_ONESHOT                       = 0x80000000
	IN_ONLYDIR                       = 0x1000000
	IN_OPEN                          = 0x20
	IN_Q_OVERFLOW                    = 0x4000
	IN_UNMOUNT                       = 0x2000
	IPPROTO_AH                       = 0x33
	IPPROTO_COMP                     = 0x6c
	IPPROTO_DCCP                     = 0x21
	IPPROTO_DSTOPTS                  = 0x3c
	IPPROTO_EGP                      = 0x8
	IPPROTO_ENCAP                    = 0x62
	IPPROTO_ESP                      = 0x32
	IPPROTO_FRAGMENT                 = 0x2c
	IPPROTO_GRE                      = 0x2f
	IPPROTO_HOPOPTS                  = 0x0
	IPPROTO_ICMP                     = 0x1
	IPPROTO_ICMPV6                   = 0x3a
	IPPROTO_IDP                      = 0x16
	IPPROTO_IGMP                     = 0x2
	IPPROTO_IP                       = 0x0
	IPPROTO_IPIP                     = 0x4
	IPPROTO_IPV6                     = 0x29
	IPPROTO_MTP                      = 0x5c
	IPPROTO_NONE                     = 0x3b
	IPPROTO_PIM                      = 0x67
	IPPROTO_PUP                      = 0xc
	IPPROTO_RAW                      = 0xff
	IPPROTO_ROUTING                  = 0x2b
	IPPROTO_RSVP                     = 0x2e
	IPPROTO_SCTP                     = 0x84
	IPPROTO_TCP                      = 0x6
	IPPROTO_TP                       = 0x1d
	IPPROTO_UDP                      = 0x11
	IPPROTO_UDPLITE                  = 0x88
	IPV6_2292DSTOPTS                 = 0x4
	IPV6_2292HOPLIMIT                = 0x8
	IPV6_2292HOPOPTS                 = 0x3
	IPV6_2292PKTINFO                 = 0x2
	IPV6_2292PKTOPTIONS              = 0x6
	IPV6_2292RTHDR                   = 0x5
	IPV6_ADDRFORM                    = 0x1
	IPV6_ADD_MEMBERSHIP              = 0x14
	IPV6_AUTHHDR                     = 0xa
	IPV6_CHECKSUM                    = 0x7
	IPV6_DROP_MEMBERSHIP             = 0x15
	IPV6_DSTOPTS                     = 0x3b
	IPV6_HOPLIMIT                    = 0x34
	IPV6_HOPOPTS                     = 0x36
	IPV6_IPSEC_POLICY                = 0x22
	IPV6_JOIN_ANYCAST                = 0x1b
	IPV6_JOIN_GROUP                  = 0x14
	IPV6_LEAVE_ANYCAST               = 0x1c
	IPV6_LEAVE_GROUP                 = 0x15
	IPV6_MTU                         = 0x18
	IPV6_MTU_DISCOVER                = 0x17
	IPV6_MULTICAST_HOPS              = 0x12
	IPV6_MULTICAST_IF                = 0x11
	IPV6_MULTICAST_LOOP              = 0x13
	IPV6_NEXTHOP                     = 0x9
	IPV6_PKTINFO                     = 0x32
	IPV6_PMTUDISC_DO                 = 0x2
	IPV6_PMTUDISC_DONT               = 0x0
	IPV6_PMTUDISC_PROBE              = 0x3
	IPV6_PMTUDISC_WANT               = 0x1
	IPV6_RECVDSTOPTS                 = 0x3a
	IPV6_RECVERR                     = 0x19
	IPV6_RECVHOPLIMIT                = 0x33
	IPV6_RECVHOPOPTS                 = 0x35
	IPV6_RECVPKTINFO                 = 0x31
	IPV6_RECVRTHDR                   = 0x38
	IPV6_RECVTCLASS                  = 0x42
	IPV6_ROUTER_ALERT                = 0x16
	IPV6_RTHDR                       = 0x39
	IPV6_RTHDRDSTOPTS                = 0x37
	IPV6_RTHDR_LOOSE                 = 0x0
	IPV6_RTHDR_STRICT                = 0x1
	IPV6_RTHDR_TYPE_0                = 0x0
	IPV6_RXDSTOPTS                   = 0x3b
	IPV6_RXHOPOPTS                   = 0x36
	IPV6_TCLASS                      = 0x43
	IPV6_UNICAST_HOPS                = 0x10
	IPV6_V6ONLY                      = 0x1a
	IPV6_XFRM_POLICY                 = 0x23
	IP_ADD_MEMBERSHIP                = 0x23
	IP_ADD_SOURCE_MEMBERSHIP         = 0x27
	IP_BLOCK_SOURCE                  = 0x26
	IP_DEFAULT_MULTICAST_LOOP        = 0x1
	IP_DEFAULT_MULTICAST_TTL         = 0x1
	IP_DF                            = 0x4000
	IP_DROP_MEMBERSHIP               = 0x24
	IP_DROP_SOURCE_MEMBERSHIP        = 0x28
	IP_FREEBIND                      = 0xf
	IP_HDRINCL                       = 0x3
	IP_IPSEC_POLICY                  = 0x10
	IP_MAXPACKET                     = 0xffff
	IP_MAX_MEMBERSHIPS               = 0x14
	IP_MF                            = 0x2000
	IP_MINTTL                        = 0x15
	IP_MSFILTER                      = 0x29
	IP_MSS                           = 0x240
	IP_MTU                           = 0xe
	IP_MTU_DISCOVER                  = 0xa
	IP_MULTICAST_IF                  = 0x20
	IP_MULTICAST_LOOP                = 0x22
	IP_MULTICAST_TTL                 = 0x21
	IP_OFFMASK                       = 0x1fff
	IP_OPTIONS                       = 0x4
	IP_ORIGDSTADDR                   = 0x14
	IP_PASSSEC                       = 0x12
	IP_PKTINFO                       = 0x8
	IP_PKTOPTIONS                    = 0x9
	IP_PMTUDISC                      = 0xa
	IP_PMTUDISC_DO                   = 0x2
	IP_PMTUDISC_DONT                 = 0x0
	IP_PMTUDISC_PROBE                = 0x3
	IP_PMTUDISC_WANT                 = 0x1
	IP_RECVERR                       = 0xb
	IP_RECVOPTS                      = 0x6
	IP_RECVORIGDSTADDR               = 0x14
	IP_RECVRETOPTS                   = 0x7
	IP_RECVTOS                       = 0xd
	IP_RECVTTL                       = 0xc
	IP_RETOPTS                       = 0x7
	IP_RF                            = 0x8000
	IP_ROUTER_ALERT                  = 0x5
	IP_TOS                           = 0x1
	IP_TRANSPARENT                   = 0x13
	IP_TTL                           = 0x2
	IP_UNBLOCK_SOURCE                = 0x25
	IP_XFRM_POLICY                   = 0x11
	LINUX_REBOOT_CMD_CAD_OFF         = 0x0
	LINUX_REBOOT_CMD_CAD_ON          = 0x89abcdef
	LINUX_REBOOT_CMD_HALT            = 0xcdef0123
	LINUX_REBOOT_CMD_KEXEC           = 0x45584543
	LINUX_REBOOT_CMD_POWER_OFF       = 0x4321fedc
	LINUX_REBOOT_CMD_RESTART         = 0x1234567
	LINUX_REBOOT_CMD_RESTART2        = 0xa1b2c3d4
	LINUX_REBOOT_CMD_SW_SUSPEND      = 0xd000fce2
	LINUX_REBOOT_MAGIC1              = 0xfee1dead
	LINUX_REBOOT_MAGIC2              = 0x28121969
	LOCK_EX                          = 0x2
	LOCK_NB                          = 0x4
	LOCK_SH                          = 0x1
	LOCK_UN                          = 0x8
	MADV_DOFORK                      = 0xb
	MADV_DONTFORK                    = 0xa
	MADV_DONTNEED                    = 0x4
	MADV_HUGEPAGE                    = 0xe
	MADV_HWPOISON                    = 0x64
	MADV_MERGEABLE                   = 0xc
	MADV_NOHUGEPAGE                  = 0xf
	MADV_NORMAL                      = 0x0
	MADV_RANDOM                      = 0x1
	MADV_REMOVE                      = 0x9
	MADV_SEQUENTIAL                  = 0x2
	MADV_UNMERGEABLE                 = 0xd
	MADV_WILLNEED                    = 0x3
	MAP_32BIT                        = 0x40
	MAP_ANON                         = 0x20
	MAP_ANONYMOUS                    = 0x20
	MAP_DENYWRITE                    = 0x800
	MAP_EXECUTABLE                   = 0x1000
	MAP_FILE                         = 0x0
	MAP_FIXED                        = 0x10
	MAP_GROWSDOWN                    = 0x100
	MAP_HUGETLB                      = 0x40000
	MAP_LOCKED                       = 0x2000
	MAP_NONBLOCK                     = 0x10000
	MAP_NORESERVE                    = 0x4000
	MAP_POPULATE                     = 0x8000
	MAP_PRIVATE                      = 0x2
	MAP_SHARED                       = 0x1
	MAP_STACK                        = 0x20000
	MAP_TYPE                         = 0xf
	MCL_CURRENT                      = 0x1
	MCL_FUTURE                       = 0x2
	MNT_DETACH                       = 0x2
	MNT_EXPIRE                       = 0x4
	MNT_FORCE                        = 0x1
	MSG_CMSG_CLOEXEC                 = 0x40000000
	MSG_CONFIRM                      = 0x800
	MSG_CTRUNC                       = 0x8
	MSG_DONTROUTE                    = 0x4
	MSG_DONTWAIT                     = 0x40
	MSG_EOR                          = 0x80
	MSG_ERRQUEUE                     = 0x2000
	MSG_FASTOPEN                     = 0x20000000
	MSG_FIN                          = 0x200
	MSG_MORE                         = 0x8000
	MSG_NOSIGNAL                     = 0x4000
	MSG_OOB                          = 0x1
	MSG_PEEK                         = 0x2
	MSG_PROXY                        = 0x10
	MSG_RST                          = 0x1000
	MSG_SYN                          = 0x400
	MSG_TRUNC                        = 0x20
	MSG_TRYHARD                      = 0x4
	MSG_WAITALL                      = 0x100
	MSG_WAITFORONE                   = 0x10000
	MS_ACTIVE                        = 0x40000000
	MS_ASYNC                         = 0x1
	MS_BIND                          = 0x1000
	MS_DIRSYNC                       = 0x80
	MS_INVALIDATE                    = 0x2
	MS_I_VERSION                     = 0x800000
	MS_KERNMOUNT                     = 0x400000
	MS_MANDLOCK                      = 0x40
	MS_MGC_MSK                       = 0xffff0000
	MS_MGC_VAL                       = 0xc0ed0000
	MS_MOVE                          = 0x2000
	MS_NOATIME                       = 0x400
	MS_NODEV                         = 0x4
	MS_NODIRATIME                    = 0x800
	MS_NOEXEC                        = 0x8
	MS_NOSUID                        = 0x2
	MS_NOUSER                        = -0x80000000
	MS_POSIXACL                      = 0x10000
	MS_PRIVATE                       = 0x40000
	MS_RDONLY                        = 0x1
	MS_REC                           = 0x4000
	MS_RELATIME                      = 0x200000
	MS_REMOUNT                       = 0x20
	MS_RMT_MASK                      = 0x800051
	MS_SHARED                        = 0x100000
	MS_SILENT                        = 0x8000
	MS_SLAVE                         = 0x80000
	MS_STRICTATIME                   = 0x1000000
	MS_SYNC                          = 0x4
	MS_SYNCHRONOUS                   = 0x10
	MS_UNBINDABLE                    = 0x20000
	NAME_MAX                         = 0xff
	NETLINK_ADD_MEMBERSHIP           = 0x1
	NETLINK_AUDIT                    = 0x9
	NETLINK_BROADCAST_ERROR          = 0x4
	NETLINK_CONNECTOR                = 0xb
	NETLINK_DNRTMSG                  = 0xe
	NETLINK_DROP_MEMBERSHIP          = 0x2
	NETLINK_ECRYPTFS                 = 0x13
	NETLINK_FIB_LOOKUP               = 0xa
	NETLINK_FIREWALL                 = 0x3
	NETLINK_GENERIC                  = 0x10
	NETLINK_INET_DIAG                = 0x4
	NETLINK_IP6_FW                   = 0xd
	NETLINK_ISCSI                    = 0x8
	NETLINK_KOBJECT_UEVENT           = 0xf
	NETLINK_NETFILTER                = 0xc
	NETLINK_NFLOG                    = 0x5
	NETLINK_NO_ENOBUFS               = 0x5
	NETLINK_PKTINFO                  = 0x3
	NETLINK_ROUTE                    = 0x0
	NETLINK_SCSITRANSPORT            = 0x12
	NETLINK_SELINUX                  = 0x7
	NETLINK_UNUSED                   = 0x1
	NETLINK_USERSOCK                 = 0x2
	NETLINK_XFRM                     = 0x6
	NLA_ALIGNTO                      = 0x4
	NLA_F_NESTED                     = 0x8000
	NLA_F_NET_BYTEORDER              = 0x4000
	NLA_HDRLEN                       = 0x4
	NLMSG_ALIGNTO                    = 0x4
	NLMSG_DONE                       = 0x3
	NLMSG_ERROR                      = 0x2
	NLMSG_HDRLEN                     = 0x10
	NLMSG_MIN_TYPE                   = 0x10
	NLMSG_NOOP                       = 0x1
	NLMSG_OVERRUN                    = 0x4
	NLM_F_ACK                        = 0x4
	NLM_F_APPEND                     = 0x800
	NLM_F_ATOMIC                     = 0x400
	NLM_F_CREATE                     = 0x400
	NLM_F_DUMP                       = 0x300
	NLM_F_ECHO                       = 0x8
	NLM_F_EXCL                       = 0x200
	NLM_F_MATCH                      = 0x200
	NLM_F_MULTI                      = 0x2
	NLM_F_REPLACE                    = 0x100
	NLM_F_REQUEST                    = 0x1
	NLM_F_ROOT                       = 0x100
	O_ACCMODE                        = 0x3
	O_APPEND                         = 0x400
	O_ASYNC                          = 0x2000
	O_CLOEXEC                        = 0x80000
	O_CREAT                          = 0x40
	O_DIRECT                         = 0x4000
	O_DIRECTORY                      = 0x10000
	O_DSYNC                          = 0x1000
	O_EXCL                           = 0x80
	O_FSYNC                          = 0x101000
	O_LARGEFILE                      = 0x0
	O_NDELAY                         = 0x800
	O_NOATIME                        = 0x40000
	O_NOCTTY                         = 0x100
	O_NOFOLLOW                       = 0x20000
	O_NONBLOCK                       = 0x800
	O_RDONLY                         = 0x0
	O_RDWR                           = 0x2
	O_RSYNC                          = 0x101000
	O_SYNC                           = 0x101000
	O_TRUNC                          = 0x200
	O_WRONLY                         = 0x1
	PACKET_ADD_MEMBERSHIP            = 0x1
	PACKET_BROADCAST                 = 0x1
	PACKET_DROP_MEMBERSHIP           = 0x2
	PACKET_FASTROUTE                 = 0x6
	PACKET_HOST                      = 0x0
	PACKET_LOOPBACK                  = 0x5
	PACKET_MR_ALLMULTI               = 0x2
	PACKET_MR_MULTICAST              = 0x0
	PACKET_MR_PROMISC                = 0x1
	PACKET_MULTICAST                 = 0x2
	PACKET_OTHERHOST                 = 0x3
	PACKET_OUTGOING                  = 0x4
	PACKET_RECV_OUTPUT               = 0x3
	PACKET_RX_RING                   = 0x5
	PACKET_STATISTICS                = 0x6
	PRIO_PGRP                        = 0x1
	PRIO_PROCESS                     = 0x0
	PRIO_USER                        = 0x2
	PROT_EXEC                        = 0x4
	PROT_GROWSDOWN                   = 0x1000000
	PROT_GROWSUP                     = 0x2000000
	PROT_NONE                        = 0x0
	PROT_READ                        = 0x1
	PROT_WRITE                       = 0x2
	PR_CAPBSET_DROP                  = 0x18
	PR_CAPBSET_READ                  = 0x17
	PR_ENDIAN_BIG                    = 0x0
	PR_ENDIAN_LITTLE                 = 0x1
	PR_ENDIAN_PPC_LITTLE             = 0x2
	PR_FPEMU_NOPRINT                 = 0x1
	PR_FPEMU_SIGFPE                  = 0x2
	PR_FP_EXC_ASYNC                  = 0x2
	PR_FP_EXC_DISABLED               = 0x0
	PR_FP_EXC_DIV                    = 0x10000
	PR_FP_EXC_INV                    = 0x100000
	PR_FP_EXC_NONRECOV               = 0x1
	PR_FP_EXC_OVF                    = 0x20000
	PR_FP_EXC_PRECISE                = 0x3
	PR_FP_EXC_RES                    = 0x80000
	PR_FP_EXC_SW_ENABLE              = 0x80
	PR_FP_EXC_UND                    = 0x40000
	PR_GET_DUMPABLE                  = 0x3
	PR_GET_ENDIAN                    = 0x13
	PR_GET_FPEMU                     = 0x9
	PR_GET_FPEXC                     = 0xb
	PR_GET_KEEPCAPS                  = 0x7
	PR_GET_NAME                      = 0x10
	PR_GET_PDEATHSIG                 = 0x2
	PR_GET_SECCOMP                   = 0x15
	PR_GET_SECUREBITS                = 0x1b
	PR_GET_TIMERSLACK                = 0x1e
	PR_GET_TIMING                    = 0xd
	PR_GET_TSC                       = 0x19
	PR_GET_UNALIGN                   = 0x5
	PR_MCE_KILL                      = 0x21
	PR_MCE_KILL_CLEAR                = 0x0
	PR_MCE_KILL_DEFAULT              = 0x2
	PR_MCE_KILL_EARLY                = 0x1
	PR_MCE_KILL_GET                  = 0x22
	PR_MCE_KILL_LATE                 = 0x0
	PR_MCE_KILL_SET                  = 0x1
	PR_SET_DUMPABLE                  = 0x4
	PR_SET_ENDIAN                    = 0x14
	PR_SET_FPEMU                     = 0xa
	PR_SET_FPEXC                     = 0xc
	PR_SET_KEEPCAPS                  = 0x8
	PR_SET_NAME                      = 0xf
	PR_SET_PDEATHSIG                 = 0x1
	PR_SET_PTRACER                   = 0x59616d61
	PR_SET_SECCOMP                   = 0x16
	PR_SET_SECUREBITS                = 0x1c
	PR_SET_TIMERSLACK                = 0x1d
	PR_SET_TIMING                    = 0xe
	PR_SET_TSC                       = 0x1a
	PR_SET_UNALIGN                   = 0x6
	PR_TASK_PERF_EVENTS_DISABLE      = 0x1f
	PR_TASK_PERF_EVENTS_ENABLE       = 0x20
	PR_TIMING_STATISTICAL            = 0x0
	PR_TIMING_TIMESTAMP              = 0x1
	PR_TSC_ENABLE                    = 0x1
	PR_TSC_SIGSEGV                   = 0x2
	PR_UNALIGN_NOPRINT               = 0x1
	PR_UNALIGN_SIGBUS                = 0x2
	PTRACE_ARCH_PRCTL                = 0x1e
	PTRACE_ATTACH                    = 0x10
	PTRACE_CONT                      = 0x7
	PTRACE_DETACH                    = 0x11
	PTRACE_EVENT_CLONE               = 0x3
	PTRACE_EVENT_EXEC                = 0x4
	PTRACE_EVENT_EXIT                = 0x6
	PTRACE_EVENT_FORK                = 0x1
	PTRACE_EVENT_VFORK               = 0x2
	PTRACE_EVENT_VFORK_DONE          = 0x5
	PTRACE_GETEVENTMSG               = 0x4201
	PTRACE_GETFPREGS                 = 0xe
	PTRACE_GETFPXREGS                = 0x12
	PTRACE_GETREGS                   = 0xc
	PTRACE_GETREGSET                 = 0x4204
	PTRACE_GETSIGINFO                = 0x4202
	PTRACE_GET_THREAD_AREA           = 0x19
	PTRACE_KILL                      = 0x8
	PTRACE_OLDSETOPTIONS             = 0x15
	PTRACE_O_MASK                    = 0x7f
	PTRACE_O_TRACECLONE              = 0x8
	PTRACE_O_TRACEEXEC               = 0x10
	PTRACE_O_TRACEEXIT               = 0x40
	PTRACE_O_TRACEFORK               = 0x2
	PTRACE_O_TRACESYSGOOD            = 0x1
	PTRACE_O_TRACEVFORK              = 0x4
	PTRACE_O_TRACEVFORKDONE          = 0x20
	PTRACE_PEEKDATA                  = 0x2
	PTRACE_PEEKTEXT                  = 0x1
	PTRACE_PEEKUSR                   = 0x3
	PTRACE_POKEDATA                  = 0x5
	PTRACE_POKETEXT                  = 0x4
	PTRACE_POKEUSR                   = 0x6
	PTRACE_SETFPREGS                 = 0xf
	PTRACE_SETFPXREGS                = 0x13
	PTRACE_SETOPTIONS                = 0x4200
	PTRACE_SETREGS                   = 0xd
	PTRACE_SETREGSET                 = 0x4205
	PTRACE_SETSIGINFO                = 0x4203
	PTRACE_SET_THREAD_AREA           = 0x1a
	PTRACE_SINGLEBLOCK               = 0x21
	PTRACE_SINGLESTEP                = 0x9
	PTRACE_SYSCALL                   = 0x18
	PTRACE_SYSEMU                    = 0x1f
	PTRACE_SYSEMU_SINGLESTEP         = 0x20
	PTRACE_TRACEME                   = 0x0
	RLIMIT_AS                        = 0x9
	RLIMIT_CORE                      = 0x4
	RLIMIT_CPU                       = 0x0
	RLIMIT_DATA                      = 0x2
	RLIMIT_FSIZE                     = 0x1
	RLIMIT_NOFILE                    = 0x7
	RLIMIT_STACK                     = 0x3
	RLIM_INFINITY                    = -0x1
	RTAX_ADVMSS                      = 0x8
	RTAX_CWND                        = 0x7
	RTAX_FEATURES                    = 0xc
	RTAX_FEATURE_ALLFRAG             = 0x8
	RTAX_FEATURE_ECN                 = 0x1
	RTAX_FEATURE_SACK                = 0x2
	RTAX_FEATURE_TIMESTAMP           = 0x4
	RTAX_HOPLIMIT                    = 0xa
	RTAX_INITCWND                    = 0xb
	RTAX_INITRWND                    = 0xe
	RTAX_LOCK                        = 0x1
	RTAX_MAX                         = 0xe
	RTAX_MTU                         = 0x2
	RTAX_REORDERING                  = 0x9
	RTAX_RTO_MIN                     = 0xd
	RTAX_RTT                         = 0x4
	RTAX_RTTVAR                      = 0x5
	RTAX_SSTHRESH                    = 0x6
	RTAX_UNSPEC                      = 0x0
	RTAX_WINDOW                      = 0x3
	RTA_ALIGNTO                      = 0x4
	RTA_MAX                          = 0x10
	RTCF_DIRECTSRC                   = 0x4000000
	RTCF_DOREDIRECT                  = 0x1000000
	RTCF_LOG                         = 0x2000000
	RTCF_MASQ                        = 0x400000
	RTCF_NAT                         = 0x800000
	RTCF_VALVE                       = 0x200000
	RTF_ADDRCLASSMASK                = 0xf8000000
	RTF_ADDRCONF                     = 0x40000
	RTF_ALLONLINK                    = 0x20000
	RTF_BROADCAST                    = 0x10000000
	RTF_CACHE                        = 0x1000000
	RTF_DEFAULT                      = 0x10000
	RTF_DYNAMIC                      = 0x10
	RTF_FLOW                         = 0x2000000
	RTF_GATEWAY                      = 0x2
	RTF_HOST                         = 0x4
	RTF_INTERFACE                    = 0x40000000
	RTF_IRTT                         = 0x100
	RTF_LINKRT                       = 0x100000
	RTF_LOCAL                        = 0x80000000
	RTF_MODIFIED                     = 0x20
	RTF_MSS                          = 0x40
	RTF_MTU                          = 0x40
	RTF_MULTICAST                    = 0x20000000
	RTF_NAT                          = 0x8000000
	RTF_NOFORWARD                    = 0x1000
	RTF_NONEXTHOP                    = 0x200000
	RTF_NOPMTUDISC                   = 0x4000
	RTF_POLICY                       = 0x4000000
	RTF_REINSTATE                    = 0x8
	RTF_REJECT                       = 0x200
	RTF_STATIC                       = 0x400
	RTF_THROW                        = 0x2000
	RTF_UP                           = 0x1
	RTF_WINDOW                       = 0x80
	RTF_XRESOLVE                     = 0x800
	RTM_BASE                         = 0x10
	RTM_DELACTION                    = 0x31
	RTM_DELADDR                      = 0x15
	RTM_DELADDRLABEL                 = 0x49
	RTM_DELLINK                      = 0x11
	RTM_DELNEIGH                     = 0x1d
	RTM_DELQDISC                     = 0x25
	RTM_DELROUTE                     = 0x19
	RTM_DELRULE                      = 0x21
	RTM_DELTCLASS                    = 0x29
	RTM_DELTFILTER                   = 0x2d
	RTM_F_CLONED                     = 0x200
	RTM_F_EQUALIZE                   = 0x400
	RTM_F_NOTIFY                     = 0x100
	RTM_F_PREFIX                     = 0x800
	RTM_GETACTION                    = 0x32
	RTM_GETADDR                      = 0x16
	RTM_GETADDRLABEL                 = 0x4a
	RTM_GETANYCAST                   = 0x3e
	RTM_GETDCB                       = 0x4e
	RTM_GETLINK                      = 0x12
	RTM_GETMULTICAST                 = 0x3a
	RTM_GETNEIGH                     = 0x1e
	RTM_GETNEIGHTBL                  = 0x42
	RTM_GETQDISC                     = 0x26
	RTM_GETROUTE                     = 0x1a
	RTM_GETRULE                      = 0x22
	RTM_GETTCLASS                    = 0x2a
	RTM_GETTFILTER                   = 0x2e
	RTM_MAX                          = 0x4f
	// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
	// IFA/IFLA netlink attribute constants required by the real net
	// package (interface_linux.go). From gc 1.27.1 syscall/ztypes_linux_amd64.go.
	IFA_UNSPEC                       = 0x0
	IFA_ADDRESS                      = 0x1
	IFA_LOCAL                        = 0x2
	IFA_LABEL                        = 0x3
	IFA_BROADCAST                    = 0x4
	IFA_ANYCAST                      = 0x5
	IFA_CACHEINFO                    = 0x6
	IFA_MULTICAST                    = 0x7
	IFLA_UNSPEC                      = 0x0
	IFLA_ADDRESS                     = 0x1
	IFLA_BROADCAST                   = 0x2
	IFLA_IFNAME                      = 0x3
	IFLA_MTU                         = 0x4
	IFLA_LINK                        = 0x5
	RTM_NEWACTION                    = 0x30
	RTM_NEWADDR                      = 0x14
	RTM_NEWADDRLABEL                 = 0x48
	RTM_NEWLINK                      = 0x10
	RTM_NEWNDUSEROPT                 = 0x44
	RTM_NEWNEIGH                     = 0x1c
	RTM_NEWNEIGHTBL                  = 0x40
	RTM_NEWPREFIX                    = 0x34
	RTM_NEWQDISC                     = 0x24
	RTM_NEWROUTE                     = 0x18
	RTM_NEWRULE                      = 0x20
	RTM_NEWTCLASS                    = 0x28
	RTM_NEWTFILTER                   = 0x2c
	RTM_NR_FAMILIES                  = 0x10
	RTM_NR_MSGTYPES                  = 0x40
	RTM_SETDCB                       = 0x4f
	RTM_SETLINK                      = 0x13
	RTM_SETNEIGHTBL                  = 0x43
	RTNH_ALIGNTO                     = 0x4
	RTNH_F_DEAD                      = 0x1
	RTNH_F_ONLINK                    = 0x4
	RTNH_F_PERVASIVE                 = 0x2
	RTN_MAX                          = 0xb
	RTPROT_BIRD                      = 0xc
	RTPROT_BOOT                      = 0x3
	RTPROT_DHCP                      = 0x10
	RTPROT_DNROUTED                  = 0xd
	RTPROT_GATED                     = 0x8
	RTPROT_KERNEL                    = 0x2
	RTPROT_MRT                       = 0xa
	RTPROT_NTK                       = 0xf
	RTPROT_RA                        = 0x9
	RTPROT_REDIRECT                  = 0x1
	RTPROT_STATIC                    = 0x4
	RTPROT_UNSPEC                    = 0x0
	RTPROT_XORP                      = 0xe
	RTPROT_ZEBRA                     = 0xb
	RT_CLASS_DEFAULT                 = 0xfd
	RT_CLASS_LOCAL                   = 0xff
	RT_CLASS_MAIN                    = 0xfe
	RT_CLASS_MAX                     = 0xff
	RT_CLASS_UNSPEC                  = 0x0
	RUSAGE_CHILDREN                  = -0x1
	RUSAGE_SELF                      = 0x0
	RUSAGE_THREAD                    = 0x1
	SCM_CREDENTIALS                  = 0x2
	SCM_RIGHTS                       = 0x1
	SCM_TIMESTAMP                    = 0x1d
	SCM_TIMESTAMPING                 = 0x25
	SCM_TIMESTAMPNS                  = 0x23
	SHUT_RD                          = 0x0
	SHUT_RDWR                        = 0x2
	SHUT_WR                          = 0x1
	SIOCADDDLCI                      = 0x8980
	SIOCADDMULTI                     = 0x8931
	SIOCADDRT                        = 0x890b
	SIOCATMARK                       = 0x8905
	SIOCDARP                         = 0x8953
	SIOCDELDLCI                      = 0x8981
	SIOCDELMULTI                     = 0x8932
	SIOCDELRT                        = 0x890c
	SIOCDEVPRIVATE                   = 0x89f0
	SIOCDIFADDR                      = 0x8936
	SIOCDRARP                        = 0x8960
	SIOCGARP                         = 0x8954
	SIOCGIFADDR                      = 0x8915
	SIOCGIFBR                        = 0x8940
	SIOCGIFBRDADDR                   = 0x8919
	SIOCGIFCONF                      = 0x8912
	SIOCGIFCOUNT                     = 0x8938
	SIOCGIFDSTADDR                   = 0x8917
	SIOCGIFENCAP                     = 0x8925
	SIOCGIFFLAGS                     = 0x8913
	SIOCGIFHWADDR                    = 0x8927
	SIOCGIFINDEX                     = 0x8933
	SIOCGIFMAP                       = 0x8970
	SIOCGIFMEM                       = 0x891f
	SIOCGIFMETRIC                    = 0x891d
	SIOCGIFMTU                       = 0x8921
	SIOCGIFNAME                      = 0x8910
	SIOCGIFNETMASK                   = 0x891b
	SIOCGIFPFLAGS                    = 0x8935
	SIOCGIFSLAVE                     = 0x8929
	SIOCGIFTXQLEN                    = 0x8942
	SIOCGPGRP                        = 0x8904
	SIOCGRARP                        = 0x8961
	SIOCGSTAMP                       = 0x8906
	SIOCGSTAMPNS                     = 0x8907
	SIOCPROTOPRIVATE                 = 0x89e0
	SIOCRTMSG                        = 0x890d
	SIOCSARP                         = 0x8955
	SIOCSIFADDR                      = 0x8916
	SIOCSIFBR                        = 0x8941
	SIOCSIFBRDADDR                   = 0x891a
	SIOCSIFDSTADDR                   = 0x8918
	SIOCSIFENCAP                     = 0x8926
	SIOCSIFFLAGS                     = 0x8914
	SIOCSIFHWADDR                    = 0x8924
	SIOCSIFHWBROADCAST               = 0x8937
	SIOCSIFLINK                      = 0x8911
	SIOCSIFMAP                       = 0x8971
	SIOCSIFMEM                       = 0x8920
	SIOCSIFMETRIC                    = 0x891e
	SIOCSIFMTU                       = 0x8922
	SIOCSIFNAME                      = 0x8923
	SIOCSIFNETMASK                   = 0x891c
	SIOCSIFPFLAGS                    = 0x8934
	SIOCSIFSLAVE                     = 0x8930
	SIOCSIFTXQLEN                    = 0x8943
	SIOCSPGRP                        = 0x8902
	SIOCSRARP                        = 0x8962
	SOCK_CLOEXEC                     = 0x80000
	SOCK_DCCP                        = 0x6
	SOCK_DGRAM                       = 0x2
	SOCK_NONBLOCK                    = 0x800
	SOCK_PACKET                      = 0xa
	SOCK_RAW                         = 0x3
	SOCK_RDM                         = 0x4
	SOCK_SEQPACKET                   = 0x5
	SOCK_STREAM                      = 0x1
	SOL_AAL                          = 0x109
	SOL_ATM                          = 0x108
	SOL_DECNET                       = 0x105
	SOL_ICMPV6                       = 0x3a
	SOL_IP                           = 0x0
	SOL_IPV6                         = 0x29
	SOL_IRDA                         = 0x10a
	SOL_PACKET                       = 0x107
	SOL_RAW                          = 0xff
	SOL_SOCKET                       = 0x1
	SOL_TCP                          = 0x6
	SOL_X25                          = 0x106
	SOMAXCONN                        = 0x80
	SO_ACCEPTCONN                    = 0x1e
	SO_ATTACH_FILTER                 = 0x1a
	SO_BINDTODEVICE                  = 0x19
	SO_BROADCAST                     = 0x6
	SO_BSDCOMPAT                     = 0xe
	SO_DEBUG                         = 0x1
	SO_DETACH_FILTER                 = 0x1b
	SO_DOMAIN                        = 0x27
	SO_DONTROUTE                     = 0x5
	SO_ERROR                         = 0x4
	SO_KEEPALIVE                     = 0x9
	SO_LINGER                        = 0xd
	SO_MARK                          = 0x24
	SO_NO_CHECK                      = 0xb
	SO_OOBINLINE                     = 0xa
	SO_PASSCRED                      = 0x10
	SO_PASSSEC                       = 0x22
	SO_PEERCRED                      = 0x11
	SO_PEERNAME                      = 0x1c
	SO_PEERSEC                       = 0x1f
	SO_PRIORITY                      = 0xc
	SO_PROTOCOL                      = 0x26
	SO_RCVBUF                        = 0x8
	SO_RCVBUFFORCE                   = 0x21
	SO_RCVLOWAT                      = 0x12
	SO_RCVTIMEO                      = 0x14
	SO_REUSEADDR                     = 0x2
	SO_RXQ_OVFL                      = 0x28
	SO_SECURITY_AUTHENTICATION       = 0x16
	SO_SECURITY_ENCRYPTION_NETWORK   = 0x18
	SO_SECURITY_ENCRYPTION_TRANSPORT = 0x17
	SO_SNDBUF                        = 0x7
	SO_SNDBUFFORCE                   = 0x20
	SO_SNDLOWAT                      = 0x13
	SO_SNDTIMEO                      = 0x15
	SO_TIMESTAMP                     = 0x1d
	SO_TIMESTAMPING                  = 0x25
	SO_TIMESTAMPNS                   = 0x23
	SO_TYPE                          = 0x3
	S_BLKSIZE                        = 0x200
	S_IEXEC                          = 0x40
	S_IFBLK                          = 0x6000
	S_IFCHR                          = 0x2000
	S_IFDIR                          = 0x4000
	S_IFIFO                          = 0x1000
	S_IFLNK                          = 0xa000
	S_IFMT                           = 0xf000
	S_IFREG                          = 0x8000
	S_IFSOCK                         = 0xc000
	S_IREAD                          = 0x100
	S_IRGRP                          = 0x20
	S_IROTH                          = 0x4
	S_IRUSR                          = 0x100
	S_IRWXG                          = 0x38
	S_IRWXO                          = 0x7
	S_IRWXU                          = 0x1c0
	S_ISGID                          = 0x400
	S_ISUID                          = 0x800
	S_ISVTX                          = 0x200
	S_IWGRP                          = 0x10
	S_IWOTH                          = 0x2
	S_IWRITE                         = 0x80
	S_IWUSR                          = 0x80
	S_IXGRP                          = 0x8
	S_IXOTH                          = 0x1
	S_IXUSR                          = 0x40
	TCIFLUSH                         = 0x0
	TCIOFLUSH                        = 0x2
	TCOFLUSH                         = 0x1
	TCP_CONGESTION                   = 0xd
	TCP_CORK                         = 0x3
	TCP_DEFER_ACCEPT                 = 0x9
	TCP_INFO                         = 0xb
	TCP_KEEPCNT                      = 0x6
	TCP_KEEPIDLE                     = 0x4
	TCP_KEEPINTVL                    = 0x5
	TCP_LINGER2                      = 0x8
	TCP_MAXSEG                       = 0x2
	TCP_MAXWIN                       = 0xffff
	TCP_MAX_WINSHIFT                 = 0xe
	TCP_MD5SIG                       = 0xe
	TCP_MD5SIG_MAXKEYLEN             = 0x50
	TCP_MSS                          = 0x200
	TCP_NODELAY                      = 0x1
	TCP_QUICKACK                     = 0xc
	TCP_SYNCNT                       = 0x7
	TCP_WINDOW_CLAMP                 = 0xa
	TIOCCBRK                         = 0x5428
	TIOCCONS                         = 0x541d
	TIOCEXCL                         = 0x540c
	TIOCGDEV                         = 0x80045432
	TIOCGETD                         = 0x5424
	TIOCGICOUNT                      = 0x545d
	TIOCGLCKTRMIOS                   = 0x5456
	TIOCGPGRP                        = 0x540f
	TIOCGPTN                         = 0x80045430
	TIOCGRS485                       = 0x542e
	TIOCGSERIAL                      = 0x541e
	TIOCGSID                         = 0x5429
	TIOCGSOFTCAR                     = 0x5419
	TIOCGWINSZ                       = 0x5413
	TIOCINQ                          = 0x541b
	TIOCLINUX                        = 0x541c
	TIOCMBIC                         = 0x5417
	TIOCMBIS                         = 0x5416
	TIOCMGET                         = 0x5415
	TIOCMIWAIT                       = 0x545c
	TIOCMSET                         = 0x5418
	TIOCM_CAR                        = 0x40
	TIOCM_CD                         = 0x40
	TIOCM_CTS                        = 0x20
	TIOCM_DSR                        = 0x100
	TIOCM_DTR                        = 0x2
	TIOCM_LE                         = 0x1
	TIOCM_RI                         = 0x80
	TIOCM_RNG                        = 0x80
	TIOCM_RTS                        = 0x4
	TIOCM_SR                         = 0x10
	TIOCM_ST                         = 0x8
	TIOCNOTTY                        = 0x5422
	TIOCNXCL                         = 0x540d
	TIOCOUTQ                         = 0x5411
	TIOCPKT                          = 0x5420
	TIOCPKT_DATA                     = 0x0
	TIOCPKT_DOSTOP                   = 0x20
	TIOCPKT_FLUSHREAD                = 0x1
	TIOCPKT_FLUSHWRITE               = 0x2
	TIOCPKT_IOCTL                    = 0x40
	TIOCPKT_NOSTOP                   = 0x10
	TIOCPKT_START                    = 0x8
	TIOCPKT_STOP                     = 0x4
	TIOCSBRK                         = 0x5427
	TIOCSCTTY                        = 0x540e
	TIOCSERCONFIG                    = 0x5453
	TIOCSERGETLSR                    = 0x5459
	TIOCSERGETMULTI                  = 0x545a
	TIOCSERGSTRUCT                   = 0x5458
	TIOCSERGWILD                     = 0x5454
	TIOCSERSETMULTI                  = 0x545b
	TIOCSERSWILD                     = 0x5455
	TIOCSER_TEMT                     = 0x1
	TIOCSETD                         = 0x5423
	TIOCSIG                          = 0x40045436
	TIOCSLCKTRMIOS                   = 0x5457
	TIOCSPGRP                        = 0x5410
	TIOCSPTLCK                       = 0x40045431
	TIOCSRS485                       = 0x542f
	TIOCSSERIAL                      = 0x541f
	TIOCSSOFTCAR                     = 0x541a
	TIOCSTI                          = 0x5412
	TIOCSWINSZ                       = 0x5414
	TUNATTACHFILTER                  = 0x401054d5
	TUNDETACHFILTER                  = 0x401054d6
	TUNGETFEATURES                   = 0x800454cf
	TUNGETIFF                        = 0x800454d2
	TUNGETSNDBUF                     = 0x800454d3
	TUNGETVNETHDRSZ                  = 0x800454d7
	TUNSETDEBUG                      = 0x400454c9
	TUNSETGROUP                      = 0x400454ce
	TUNSETIFF                        = 0x400454ca
	TUNSETLINK                       = 0x400454cd
	TUNSETNOCSUM                     = 0x400454c8
	TUNSETOFFLOAD                    = 0x400454d0
	TUNSETOWNER                      = 0x400454cc
	TUNSETPERSIST                    = 0x400454cb
	TUNSETSNDBUF                     = 0x400454d4
	TUNSETTXFILTER                   = 0x400454d1
	TUNSETVNETHDRSZ                  = 0x400454d8
	WALL                             = 0x40000000
	WCLONE                           = 0x80000000
	WCONTINUED                       = 0x8
	WEXITED                          = 0x4
	WNOHANG                          = 0x1
	WNOTHREAD                        = 0x20000000
	WNOWAIT                          = 0x1000000
	WORDSIZE                         = 0x40
	WSTOPPED                         = 0x2
	WUNTRACED                        = 0x2
)

const (
	E2BIG           = Errno(0x7)
	EACCES          = Errno(0xd)
	EADDRINUSE      = Errno(0x62)
	EADDRNOTAVAIL   = Errno(0x63)
	EADV            = Errno(0x44)
	EAFNOSUPPORT    = Errno(0x61)
	EAGAIN          = Errno(0xb)
	EALREADY        = Errno(0x72)
	EBADE           = Errno(0x34)
	EBADF           = Errno(0x9)
	EBADFD          = Errno(0x4d)
	EBADMSG         = Errno(0x4a)
	EBADR           = Errno(0x35)
	EBADRQC         = Errno(0x38)
	EBADSLT         = Errno(0x39)
	EBFONT          = Errno(0x3b)
	EBUSY           = Errno(0x10)
	ECANCELED       = Errno(0x7d)
	ECHILD          = Errno(0xa)
	ECHRNG          = Errno(0x2c)
	ECOMM           = Errno(0x46)
	ECONNABORTED    = Errno(0x67)
	ECONNREFUSED    = Errno(0x6f)
	ECONNRESET      = Errno(0x68)
	EDEADLK         = Errno(0x23)
	EDEADLOCK       = Errno(0x23)
	EDESTADDRREQ    = Errno(0x59)
	EDOM            = Errno(0x21)
	EDOTDOT         = Errno(0x49)
	EDQUOT          = Errno(0x7a)
	EEXIST          = Errno(0x11)
	EFAULT          = Errno(0xe)
	EFBIG           = Errno(0x1b)
	EHOSTDOWN       = Errno(0x70)
	EHOSTUNREACH    = Errno(0x71)
	EIDRM           = Errno(0x2b)
	EILSEQ          = Errno(0x54)
	EINPROGRESS     = Errno(0x73)
	EINTR           = Errno(0x4)
	EINVAL          = Errno(0x16)
	EIO             = Errno(0x5)
	EISCONN         = Errno(0x6a)
	EISDIR          = Errno(0x15)
	EISNAM          = Errno(0x78)
	EKEYEXPIRED     = Errno(0x7f)
	EKEYREJECTED    = Errno(0x81)
	EKEYREVOKED     = Errno(0x80)
	EL2HLT          = Errno(0x33)
	EL2NSYNC        = Errno(0x2d)
	EL3HLT          = Errno(0x2e)
	EL3RST          = Errno(0x2f)
	ELIBACC         = Errno(0x4f)
	ELIBBAD         = Errno(0x50)
	ELIBEXEC        = Errno(0x53)
	ELIBMAX         = Errno(0x52)
	ELIBSCN         = Errno(0x51)
	ELNRNG          = Errno(0x30)
	ELOOP           = Errno(0x28)
	EMEDIUMTYPE     = Errno(0x7c)
	EMFILE          = Errno(0x18)
	EMLINK          = Errno(0x1f)
	EMSGSIZE        = Errno(0x5a)
	EMULTIHOP       = Errno(0x48)
	ENAMETOOLONG    = Errno(0x24)
	ENAVAIL         = Errno(0x77)
	ENETDOWN        = Errno(0x64)
	ENETRESET       = Errno(0x66)
	ENETUNREACH     = Errno(0x65)
	ENFILE          = Errno(0x17)
	ENOANO          = Errno(0x37)
	ENOBUFS         = Errno(0x69)
	ENOCSI          = Errno(0x32)
	ENODATA         = Errno(0x3d)
	ENODEV          = Errno(0x13)
	ENOENT          = Errno(0x2)
	ENOEXEC         = Errno(0x8)
	ENOKEY          = Errno(0x7e)
	ENOLCK          = Errno(0x25)
	ENOLINK         = Errno(0x43)
	ENOMEDIUM       = Errno(0x7b)
	ENOMEM          = Errno(0xc)
	ENOMSG          = Errno(0x2a)
	ENONET          = Errno(0x40)
	ENOPKG          = Errno(0x41)
	ENOPROTOOPT     = Errno(0x5c)
	ENOSPC          = Errno(0x1c)
	ENOSR           = Errno(0x3f)
	ENOSTR          = Errno(0x3c)
	ENOSYS          = Errno(0x26)
	ENOTBLK         = Errno(0xf)
	ENOTCONN        = Errno(0x6b)
	ENOTDIR         = Errno(0x14)
	ENOTEMPTY       = Errno(0x27)
	ENOTNAM         = Errno(0x76)
	ENOTRECOVERABLE = Errno(0x83)
	ENOTSOCK        = Errno(0x58)
	ENOTSUP         = Errno(0x5f)
	ENOTTY          = Errno(0x19)
	ENOTUNIQ        = Errno(0x4c)
	ENXIO           = Errno(0x6)
	EOPNOTSUPP      = Errno(0x5f)
	EOVERFLOW       = Errno(0x4b)
	EOWNERDEAD      = Errno(0x82)
	EPERM           = Errno(0x1)
	EPFNOSUPPORT    = Errno(0x60)
	EPIPE           = Errno(0x20)
	EPROTO          = Errno(0x47)
	EPROTONOSUPPORT = Errno(0x5d)
	EPROTOTYPE      = Errno(0x5b)
	ERANGE          = Errno(0x22)
	EREMCHG         = Errno(0x4e)
	EREMOTE         = Errno(0x42)
	EREMOTEIO       = Errno(0x79)
	ERESTART        = Errno(0x55)
	ERFKILL         = Errno(0x84)
	EROFS           = Errno(0x1e)
	ESHUTDOWN       = Errno(0x6c)
	ESOCKTNOSUPPORT = Errno(0x5e)
	ESPIPE          = Errno(0x1d)
	ESRCH           = Errno(0x3)
	ESRMNT          = Errno(0x45)
	ESTALE          = Errno(0x74)
	ESTRPIPE        = Errno(0x56)
	ETIME           = Errno(0x3e)
	ETIMEDOUT       = Errno(0x6e)
	ETOOMANYREFS    = Errno(0x6d)
	ETXTBSY         = Errno(0x1a)
	EUCLEAN         = Errno(0x75)
	EUNATCH         = Errno(0x31)
	EUSERS          = Errno(0x57)
	EWOULDBLOCK     = Errno(0xb)
	EXDEV           = Errno(0x12)
	EXFULL          = Errno(0x36)
)

const (
	SIGABRT   = Signal(0x6)
	SIGALRM   = Signal(0xe)
	SIGBUS    = Signal(0x7)
	SIGCHLD   = Signal(0x11)
	SIGCLD    = Signal(0x11)
	SIGCONT   = Signal(0x12)
	SIGFPE    = Signal(0x8)
	SIGHUP    = Signal(0x1)
	SIGILL    = Signal(0x4)
	SIGINT    = Signal(0x2)
	SIGIO     = Signal(0x1d)
	SIGIOT    = Signal(0x6)
	SIGKILL   = Signal(0x9)
	SIGPIPE   = Signal(0xd)
	SIGPOLL   = Signal(0x1d)
	SIGPROF   = Signal(0x1b)
	SIGPWR    = Signal(0x1e)
	SIGQUIT   = Signal(0x3)
	SIGSEGV   = Signal(0xb)
	SIGSTKFLT = Signal(0x10)
	SIGSTOP   = Signal(0x13)
	SIGSYS    = Signal(0x1f)
	SIGTERM   = Signal(0xf)
	SIGTRAP   = Signal(0x5)
	SIGTSTP   = Signal(0x14)
	SIGTTIN   = Signal(0x15)
	SIGTTOU   = Signal(0x16)
	SIGUNUSED = Signal(0x1f)
	SIGURG    = Signal(0x17)
	SIGUSR1   = Signal(0xa)
	SIGUSR2   = Signal(0xc)
	SIGVTALRM = Signal(0x1a)
	SIGWINCH  = Signal(0x1c)
	SIGXCPU   = Signal(0x18)
	SIGXFSZ   = Signal(0x19)
)

const (
	SYS_READ                   = 0
	SYS_WRITE                  = 1
	SYS_OPEN                   = 2
	SYS_CLOSE                  = 3
	SYS_STAT                   = 4
	SYS_FSTAT                  = 5
	SYS_LSTAT                  = 6
	SYS_POLL                   = 7
	SYS_LSEEK                  = 8
	SYS_MMAP                   = 9
	SYS_MPROTECT               = 10
	SYS_MUNMAP                 = 11
	SYS_BRK                    = 12
	SYS_RT_SIGACTION           = 13
	SYS_RT_SIGPROCMASK         = 14
	SYS_RT_SIGRETURN           = 15
	SYS_IOCTL                  = 16
	SYS_PREAD64                = 17
	SYS_PWRITE64               = 18
	SYS_READV                  = 19
	SYS_WRITEV                 = 20
	SYS_ACCESS                 = 21
	SYS_PIPE                   = 22
	SYS_SELECT                 = 23
	SYS_SCHED_YIELD            = 24
	SYS_MREMAP                 = 25
	SYS_MSYNC                  = 26
	SYS_MINCORE                = 27
	SYS_MADVISE                = 28
	SYS_SHMGET                 = 29
	SYS_SHMAT                  = 30
	SYS_SHMCTL                 = 31
	SYS_DUP                    = 32
	SYS_DUP2                   = 33
	SYS_PAUSE                  = 34
	SYS_NANOSLEEP              = 35
	SYS_GETITIMER              = 36
	SYS_ALARM                  = 37
	SYS_SETITIMER              = 38
	SYS_GETPID                 = 39
	SYS_SENDFILE               = 40
	SYS_SOCKET                 = 41
	SYS_CONNECT                = 42
	SYS_ACCEPT                 = 43
	SYS_SENDTO                 = 44
	SYS_RECVFROM               = 45
	SYS_SENDMSG                = 46
	SYS_RECVMSG                = 47
	SYS_SHUTDOWN               = 48
	SYS_BIND                   = 49
	SYS_LISTEN                 = 50
	SYS_GETSOCKNAME            = 51
	SYS_GETPEERNAME            = 52
	SYS_SOCKETPAIR             = 53
	SYS_SETSOCKOPT             = 54
	SYS_GETSOCKOPT             = 55
	SYS_CLONE                  = 56
	SYS_FORK                   = 57
	SYS_VFORK                  = 58
	SYS_EXECVE                 = 59
	SYS_EXIT                   = 60
	SYS_WAIT4                  = 61
	SYS_KILL                   = 62
	SYS_UNAME                  = 63
	SYS_SEMGET                 = 64
	SYS_SEMOP                  = 65
	SYS_SEMCTL                 = 66
	SYS_SHMDT                  = 67
	SYS_MSGGET                 = 68
	SYS_MSGSND                 = 69
	SYS_MSGRCV                 = 70
	SYS_MSGCTL                 = 71
	SYS_FCNTL                  = 72
	SYS_FLOCK                  = 73
	SYS_FSYNC                  = 74
	SYS_FDATASYNC              = 75
	SYS_TRUNCATE               = 76
	SYS_FTRUNCATE              = 77
	SYS_GETDENTS               = 78
	SYS_GETCWD                 = 79
	SYS_CHDIR                  = 80
	SYS_FCHDIR                 = 81
	SYS_RENAME                 = 82
	SYS_MKDIR                  = 83
	SYS_RMDIR                  = 84
	SYS_CREAT                  = 85
	SYS_LINK                   = 86
	SYS_UNLINK                 = 87
	SYS_SYMLINK                = 88
	SYS_READLINK               = 89
	SYS_CHMOD                  = 90
	SYS_FCHMOD                 = 91
	SYS_CHOWN                  = 92
	SYS_FCHOWN                 = 93
	SYS_LCHOWN                 = 94
	SYS_UMASK                  = 95
	SYS_GETTIMEOFDAY           = 96
	SYS_GETRLIMIT              = 97
	SYS_GETRUSAGE              = 98
	SYS_SYSINFO                = 99
	SYS_TIMES                  = 100
	SYS_PTRACE                 = 101
	SYS_GETUID                 = 102
	SYS_SYSLOG                 = 103
	SYS_GETGID                 = 104
	SYS_SETUID                 = 105
	SYS_SETGID                 = 106
	SYS_GETEUID                = 107
	SYS_GETEGID                = 108
	SYS_SETPGID                = 109
	SYS_GETPPID                = 110
	SYS_GETPGRP                = 111
	SYS_SETSID                 = 112
	SYS_SETREUID               = 113
	SYS_SETREGID               = 114
	SYS_GETGROUPS              = 115
	SYS_SETGROUPS              = 116
	SYS_SETRESUID              = 117
	SYS_GETRESUID              = 118
	SYS_SETRESGID              = 119
	SYS_GETRESGID              = 120
	SYS_GETPGID                = 121
	SYS_SETFSUID               = 122
	SYS_SETFSGID               = 123
	SYS_GETSID                 = 124
	SYS_CAPGET                 = 125
	SYS_CAPSET                 = 126
	SYS_RT_SIGPENDING          = 127
	SYS_RT_SIGTIMEDWAIT        = 128
	SYS_RT_SIGQUEUEINFO        = 129
	SYS_RT_SIGSUSPEND          = 130
	SYS_SIGALTSTACK            = 131
	SYS_UTIME                  = 132
	SYS_MKNOD                  = 133
	SYS_USELIB                 = 134
	SYS_PERSONALITY            = 135
	SYS_USTAT                  = 136
	SYS_STATFS                 = 137
	SYS_FSTATFS                = 138
	SYS_SYSFS                  = 139
	SYS_GETPRIORITY            = 140
	SYS_SETPRIORITY            = 141
	SYS_SCHED_SETPARAM         = 142
	SYS_SCHED_GETPARAM         = 143
	SYS_SCHED_SETSCHEDULER     = 144
	SYS_SCHED_GETSCHEDULER     = 145
	SYS_SCHED_GET_PRIORITY_MAX = 146
	SYS_SCHED_GET_PRIORITY_MIN = 147
	SYS_SCHED_RR_GET_INTERVAL  = 148
	SYS_MLOCK                  = 149
	SYS_MUNLOCK                = 150
	SYS_MLOCKALL               = 151
	SYS_MUNLOCKALL             = 152
	SYS_VHANGUP                = 153
	SYS_MODIFY_LDT             = 154
	SYS_PIVOT_ROOT             = 155
	SYS__SYSCTL                = 156
	SYS_PRCTL                  = 157
	SYS_ARCH_PRCTL             = 158
	SYS_ADJTIMEX               = 159
	SYS_SETRLIMIT              = 160
	SYS_CHROOT                 = 161
	SYS_SYNC                   = 162
	SYS_ACCT                   = 163
	SYS_SETTIMEOFDAY           = 164
	SYS_MOUNT                  = 165
	SYS_UMOUNT2                = 166
	SYS_SWAPON                 = 167
	SYS_SWAPOFF                = 168
	SYS_REBOOT                 = 169
	SYS_SETHOSTNAME            = 170
	SYS_SETDOMAINNAME          = 171
	SYS_IOPL                   = 172
	SYS_IOPERM                 = 173
	SYS_CREATE_MODULE          = 174
	SYS_INIT_MODULE            = 175
	SYS_DELETE_MODULE          = 176
	SYS_GET_KERNEL_SYMS        = 177
	SYS_QUERY_MODULE           = 178
	SYS_QUOTACTL               = 179
	SYS_NFSSERVCTL             = 180
	SYS_GETPMSG                = 181
	SYS_PUTPMSG                = 182
	SYS_AFS_SYSCALL            = 183
	SYS_TUXCALL                = 184
	SYS_SECURITY               = 185
	SYS_GETTID                 = 186
	SYS_READAHEAD              = 187
	SYS_SETXATTR               = 188
	SYS_LSETXATTR              = 189
	SYS_FSETXATTR              = 190
	SYS_GETXATTR               = 191
	SYS_LGETXATTR              = 192
	SYS_FGETXATTR              = 193
	SYS_LISTXATTR              = 194
	SYS_LLISTXATTR             = 195
	SYS_FLISTXATTR             = 196
	SYS_REMOVEXATTR            = 197
	SYS_LREMOVEXATTR           = 198
	SYS_FREMOVEXATTR           = 199
	SYS_TKILL                  = 200
	SYS_TIME                   = 201
	SYS_FUTEX                  = 202
	SYS_SCHED_SETAFFINITY      = 203
	SYS_SCHED_GETAFFINITY      = 204
	SYS_SET_THREAD_AREA        = 205
	SYS_IO_SETUP               = 206
	SYS_IO_DESTROY             = 207
	SYS_IO_GETEVENTS           = 208
	SYS_IO_SUBMIT              = 209
	SYS_IO_CANCEL              = 210
	SYS_GET_THREAD_AREA        = 211
	SYS_LOOKUP_DCOOKIE         = 212
	SYS_EPOLL_CREATE           = 213
	SYS_EPOLL_CTL_OLD          = 214
	SYS_EPOLL_WAIT_OLD         = 215
	SYS_REMAP_FILE_PAGES       = 216
	SYS_GETDENTS64             = 217
	SYS_SET_TID_ADDRESS        = 218
	SYS_RESTART_SYSCALL        = 219
	SYS_SEMTIMEDOP             = 220
	SYS_FADVISE64              = 221
	SYS_TIMER_CREATE           = 222
	SYS_TIMER_SETTIME          = 223
	SYS_TIMER_GETTIME          = 224
	SYS_TIMER_GETOVERRUN       = 225
	SYS_TIMER_DELETE           = 226
	SYS_CLOCK_SETTIME          = 227
	SYS_CLOCK_GETTIME          = 228
	SYS_CLOCK_GETRES           = 229
	SYS_CLOCK_NANOSLEEP        = 230
	SYS_EXIT_GROUP             = 231
	SYS_EPOLL_WAIT             = 232
	SYS_EPOLL_CTL              = 233
	SYS_TGKILL                 = 234
	SYS_UTIMES                 = 235
	SYS_VSERVER                = 236
	SYS_MBIND                  = 237
	SYS_SET_MEMPOLICY          = 238
	SYS_GET_MEMPOLICY          = 239
	SYS_MQ_OPEN                = 240
	SYS_MQ_UNLINK              = 241
	SYS_MQ_TIMEDSEND           = 242
	SYS_MQ_TIMEDRECEIVE        = 243
	SYS_MQ_NOTIFY              = 244
	SYS_MQ_GETSETATTR          = 245
	SYS_KEXEC_LOAD             = 246
	SYS_WAITID                 = 247
	SYS_ADD_KEY                = 248
	SYS_REQUEST_KEY            = 249
	SYS_KEYCTL                 = 250
	SYS_IOPRIO_SET             = 251
	SYS_IOPRIO_GET             = 252
	SYS_INOTIFY_INIT           = 253
	SYS_INOTIFY_ADD_WATCH      = 254
	SYS_INOTIFY_RM_WATCH       = 255
	SYS_MIGRATE_PAGES          = 256
	SYS_OPENAT                 = 257
	SYS_MKDIRAT                = 258
	SYS_MKNODAT                = 259
	SYS_FCHOWNAT               = 260
	SYS_FUTIMESAT              = 261
	SYS_NEWFSTATAT             = 262
	SYS_UNLINKAT               = 263
	SYS_RENAMEAT               = 264
	SYS_LINKAT                 = 265
	SYS_SYMLINKAT              = 266
	SYS_READLINKAT             = 267
	SYS_FCHMODAT               = 268
	SYS_FACCESSAT              = 269
	SYS_PSELECT6               = 270
	SYS_PPOLL                  = 271
	SYS_UNSHARE                = 272
	SYS_SET_ROBUST_LIST        = 273
	SYS_GET_ROBUST_LIST        = 274
	SYS_SPLICE                 = 275
	SYS_TEE                    = 276
	SYS_SYNC_FILE_RANGE        = 277
	SYS_VMSPLICE               = 278
	SYS_MOVE_PAGES             = 279
	SYS_UTIMENSAT              = 280
	SYS_EPOLL_PWAIT            = 281
	SYS_SIGNALFD               = 282
	SYS_TIMERFD_CREATE         = 283
	SYS_EVENTFD                = 284
	SYS_FALLOCATE              = 285
	SYS_TIMERFD_SETTIME        = 286
	SYS_TIMERFD_GETTIME        = 287
	SYS_ACCEPT4                = 288
	SYS_SIGNALFD4              = 289
	SYS_EVENTFD2               = 290
	SYS_EPOLL_CREATE1          = 291
	SYS_DUP3                   = 292
	SYS_PIPE2                  = 293
	SYS_INOTIFY_INIT1          = 294
	SYS_PREADV                 = 295
	SYS_PWRITEV                = 296
	SYS_RT_TGSIGQUEUEINFO      = 297
	SYS_PERF_EVENT_OPEN        = 298
	SYS_RECVMMSG               = 299
	SYS_FANOTIFY_INIT          = 300
	SYS_FANOTIFY_MARK          = 301
	SYS_PRLIMIT64              = 302
)

var errors = [...]string{
	1:   "operation not permitted",
	2:   "no such file or directory",
	3:   "no such process",
	4:   "interrupted system call",
	5:   "input/output error",
	6:   "no such device or address",
	7:   "argument list too long",
	8:   "exec format error",
	9:   "bad file descriptor",
	10:  "no child processes",
	11:  "resource temporarily unavailable",
	12:  "cannot allocate memory",
	13:  "permission denied",
	14:  "bad address",
	15:  "block device required",
	16:  "device or resource busy",
	17:  "file exists",
	18:  "invalid cross-device link",
	19:  "no such device",
	20:  "not a directory",
	21:  "is a directory",
	22:  "invalid argument",
	23:  "too many open files in system",
	24:  "too many open files",
	25:  "inappropriate ioctl for device",
	26:  "text file busy",
	27:  "file too large",
	28:  "no space left on device",
	29:  "illegal seek",
	30:  "read-only file system",
	31:  "too many links",
	32:  "broken pipe",
	33:  "numerical argument out of domain",
	34:  "numerical result out of range",
	35:  "resource deadlock avoided",
	36:  "file name too long",
	37:  "no locks available",
	38:  "function not implemented",
	39:  "directory not empty",
	40:  "too many levels of symbolic links",
	42:  "no message of desired type",
	43:  "identifier removed",
	44:  "channel number out of range",
	45:  "level 2 not synchronized",
	46:  "level 3 halted",
	47:  "level 3 reset",
	48:  "link number out of range",
	49:  "protocol driver not attached",
	50:  "no CSI structure available",
	51:  "level 2 halted",
	52:  "invalid exchange",
	53:  "invalid request descriptor",
	54:  "exchange full",
	55:  "no anode",
	56:  "invalid request code",
	57:  "invalid slot",
	59:  "bad font file format",
	60:  "device not a stream",
	61:  "no data available",
	62:  "timer expired",
	63:  "out of streams resources",
	64:  "machine is not on the network",
	65:  "package not installed",
	66:  "object is remote",
	67:  "link has been severed",
	68:  "advertise error",
	69:  "srmount error",
	70:  "communication error on send",
	71:  "protocol error",
	72:  "multihop attempted",
	73:  "RFS specific error",
	74:  "bad message",
	75:  "value too large for defined data type",
	76:  "name not unique on network",
	77:  "file descriptor in bad state",
	78:  "remote address changed",
	79:  "can not access a needed shared library",
	80:  "accessing a corrupted shared library",
	81:  ".lib section in a.out corrupted",
	82:  "attempting to link in too many shared libraries",
	83:  "cannot exec a shared library directly",
	84:  "invalid or incomplete multibyte or wide character",
	85:  "interrupted system call should be restarted",
	86:  "streams pipe error",
	87:  "too many users",
	88:  "socket operation on non-socket",
	89:  "destination address required",
	90:  "message too long",
	91:  "protocol wrong type for socket",
	92:  "protocol not available",
	93:  "protocol not supported",
	94:  "socket type not supported",
	95:  "operation not supported",
	96:  "protocol family not supported",
	97:  "address family not supported by protocol",
	98:  "address already in use",
	99:  "cannot assign requested address",
	100: "network is down",
	101: "network is unreachable",
	102: "network dropped connection on reset",
	103: "software caused connection abort",
	104: "connection reset by peer",
	105: "no buffer space available",
	106: "transport endpoint is already connected",
	107: "transport endpoint is not connected",
	108: "cannot send after transport endpoint shutdown",
	109: "too many references: cannot splice",
	110: "connection timed out",
	111: "connection refused",
	112: "host is down",
	113: "no route to host",
	114: "operation already in progress",
	115: "operation now in progress",
	116: "stale file handle",
	117: "structure needs cleaning",
	118: "not a XENIX named type file",
	119: "no XENIX semaphores available",
	120: "is a named type file",
	121: "remote I/O error",
	122: "disk quota exceeded",
	123: "no medium found",
	124: "wrong medium type",
	125: "operation canceled",
	126: "required key not available",
	127: "key has expired",
	128: "key has been revoked",
	129: "key was rejected by service",
	130: "owner died",
	131: "state not recoverable",
	132: "operation not possible due to RF-kill",
}

var signals = [...]string{
	1:  "hangup",
	2:  "interrupt",
	3:  "quit",
	4:  "illegal instruction",
	5:  "trace/breakpoint trap",
	6:  "aborted",
	7:  "bus error",
	8:  "floating point exception",
	9:  "killed",
	10: "user defined signal 1",
	11: "segmentation fault",
	12: "user defined signal 2",
	13: "broken pipe",
	14: "alarm clock",
	15: "terminated",
	16: "stack fault",
	17: "child exited",
	18: "continued",
	19: "stopped (signal)",
	20: "stopped",
	21: "stopped (tty input)",
	22: "stopped (tty output)",
	23: "urgent I/O condition",
	24: "CPU time limit exceeded",
	25: "file size limit exceeded",
	26: "virtual timer expired",
	27: "profiling timer expired",
	28: "window changed",
	29: "I/O possible",
	30: "power failure",
	31: "bad system call",
}

type Timespec struct {
	Sec  int64
	Nsec int64
}

type Timeval struct {
	Sec  int64
	Usec int64
}

type Time_t int64

type Tms struct {
	Utime  int64
	Stime  int64
	Cutime int64
	Cstime int64
}

type Utimbuf struct {
	Actime  int64
	Modtime int64
}

type Rusage struct {
	Utime    Timeval
	Stime    Timeval
	Maxrss   int64
	Ixrss    int64
	Idrss    int64
	Isrss    int64
	Minflt   int64
	Majflt   int64
	Nswap    int64
	Inblock  int64
	Oublock  int64
	Msgsnd   int64
	Msgrcv   int64
	Nsignals int64
	Nvcsw    int64
	Nivcsw   int64
}

type Rlimit struct {
	Cur uint64
	Max uint64
}

type Stat_t struct {
	Dev       uint64
	Ino       uint64
	Nlink     uint64
	Mode      uint32
	Uid       uint32
	Gid       uint32
	X__pad0   int32
	Rdev      uint64
	Size      int64
	Blksize   int64
	Blocks    int64
	Atim      Timespec
	Mtim      Timespec
	Ctim      Timespec
	X__unused [3]int64
}

type Statfs_t struct {
	Type    int64
	Bsize   int64
	Blocks  uint64
	Bfree   uint64
	Bavail  uint64
	Files   uint64
	Ffree   uint64
	Fsid    Fsid
	Namelen int64
	Frsize  int64
	Flags   int64
	Spare   [4]int64
}

type Fsid struct {
	X__val [2]int32
}

type Dirent struct {
	Ino       uint64
	Off       int64
	Reclen    uint16
	Type      uint8
	Name      [256]int8
	Pad_cgo_0 [5]byte
}

type Utsname struct {
	Sysname    [65]int8
	Nodename   [65]int8
	Release    [65]int8
	Version    [65]int8
	Machine    [65]int8
	Domainname [65]int8
}

type EpollEvent struct {
	Events uint32
	Fd     int32
	Pad    int32
}

type InotifyEvent struct {
	Wd     int32
	Mask   uint32
	Cookie uint32
	Len    uint32
	Name   [0]uint8
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Netlink types required by the real net package (interface_linux.go).
// Struct definitions for type-checking; actual netlink operations go
// through the netpoller when implemented.
type NlMsghdr struct {
	Len   uint32
	Type  uint16
	Flags uint16
	Seq   uint32
	Pid   uint32
}

type RtAttr struct {
	Len  uint16
	Type uint16
}

type IfInfomsg struct {
	Family     uint8
	X__ifi_pad uint8
	Type       uint16
	Index      int32
	Flags      uint32
	Change     uint32
}

type IfAddrmsg struct {
	Family    uint8
	Prefixlen uint8
	Flags     uint8
	Scope     uint8
	Index     uint32
}

type NetlinkMessage struct {
	Header NlMsghdr
	Data   []byte
}

type NetlinkRouteAttr struct {
	Attr  RtAttr
	Value []byte
}

const failNetlink = "syscall.Netlink*: temporary until ASM backend (his 2026-10-15 order)"

func NetlinkRIB(proto, family int) ([]byte, error) {
	panic(failNetlink)
}

func ParseNetlinkMessage(b []byte) ([]NetlinkMessage, error) {
	panic(failNetlink)
}

func ParseNetlinkRouteAttr(m *NetlinkMessage) ([]NetlinkRouteAttr, error) {
	panic(failNetlink)
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Multicast request structs required by the real net package.
// Verbatim from gc 1.27.1.
type IPMreq struct {
	Multiaddr [4]byte
	Interface [4]byte
}

type IPMreqn struct {
	Multiaddr [4]byte
	Address   [4]byte
	Ifindex   int32
}

type IPv6Mreq struct {
	Multiaddr [16]byte
	Interface uint32
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Linger struct required by the real net package (sockopt_posix.go).
// From gc 1.27.1.
type Linger struct {
	Onoff  int32
	Linger int32
}

type WaitStatus uint32

// Wait status is 7 bits at bottom, either 0 (exited),
// 0x7F (stopped), or a signal number that caused an exit.
// The 0x80 bit is whether there was a core dump.
// An extra number (exit code, signal causing a stop)
// is in the high bits. At least that's the idea.
// There are various irregularities. For example, the
// "continued" status is 0xFFFF, distinguishing itself
// from stopped via the core dump bit.

const (
	wsMask    = 0x7F
	wsCore    = 0x80
	wsExited  = 0x00
	wsStopped = 0x7F
	wsShift   = 8
)

func (w WaitStatus) Exited() bool { return w&wsMask == wsExited }

func (w WaitStatus) Signaled() bool { return w&wsMask != wsStopped && w&wsMask != wsExited }

func (w WaitStatus) Stopped() bool { return w&0xFF == wsStopped }

func (w WaitStatus) Continued() bool { return w == 0xFFFF }

func (w WaitStatus) CoreDump() bool { return w.Signaled() && w&wsCore != 0 }

func (w WaitStatus) ExitStatus() int {
	if !w.Exited() {
		return -1
	}
	return int(w>>wsShift) & 0xFF
}

func (w WaitStatus) Signal() Signal {
	if !w.Signaled() {
		return -1
	}
	return Signal(w & wsMask)
}

func (w WaitStatus) StopSignal() Signal {
	if !w.Stopped() {
		return -1
	}
	return Signal(w>>wsShift) & 0xFF
}

func (w WaitStatus) TrapCause() int {
	if w.StopSignal() != SIGTRAP {
		return -1
	}
	return int(w>>wsShift) >> 8
}

// Credential holds user and group identities for a new process.
type Credential struct {
	Uid         uint32   // User ID.
	Gid         uint32   // Group ID.
	Groups      []uint32 // Supplementary group IDs.
	NoSetGroups bool     // If true, don't set supplementary groups
}

// ProcAttr holds attributes that will be applied to a new process started
// by [StartProcess].
type ProcAttr struct {
	Dir   string   // Current working directory.
	Env   []string // Environment.
	Files []uintptr
	Sys   *SysProcAttr
}

// SysProcAttr holds attributes for Linux process creation.
// (Subset of gc's exec_linux.go SysProcAttr: the fields are vocabulary;
// StartProcess itself is fail-fast in M8.)
type SysProcAttr struct {
	Chroot     string
	Credential *Credential
	Ptrace     bool
	Setsid     bool
	Setpgid    bool
	Setctty    bool
	Noctty     bool
	Ctty       int
	Foreground bool
	Pgid       int
	Pdeathsig  Signal
	Cloneflags uintptr
}

// Sockaddr is gc's socket-address interface. The real method set needs
// unsafe (sockaddr() (unsafe.Pointer, _Socklen, error)); the synthetic
// package keeps the empty interface so call sites type-check — every
// socket operation fail-fasts at runtime (re-test: net package
// bring-up), so no method set is observable.
type Sockaddr interface{}

// SockaddrInet4/SockaddrInet6/SockaddrUnix are the public address forms
// (fields only; methods need unsafe and live with the fail-fast socket
// scope).
type SockaddrInet4 struct {
	Port int
	Addr [4]byte
}

type SockaddrInet6 struct {
	Port   int
	ZoneId uint32
	Addr   [16]byte
}

type SockaddrUnix struct {
	Name string
}

// RawConn/Conn mirror gc's syscall.Conn API (used by os.NewFile's
// SyscallConn, which fail-fasts in M8).
type RawConn interface {
	Control(f func(fd uintptr)) error
	Read(f func(fd uintptr) bool) error
	Write(f func(fd uintptr) bool) error
}

type Conn interface {
	SyscallConn() (RawConn, error)
}

func setTimespec(sec, nsec int64) Timespec {
	return Timespec{Sec: sec, Nsec: nsec}
}

func setTimeval(sec, usec int64) Timeval {
	return Timeval{Sec: sec, Usec: usec}
}

// Unix returns the time stored in ts as seconds plus nanoseconds.
func (ts *Timespec) Unix() (sec int64, nsec int64) {
	return int64(ts.Sec), int64(ts.Nsec)
}

// Unix returns the time stored in tv as seconds plus nanoseconds.
func (tv *Timeval) Unix() (sec int64, nsec int64) {
	return int64(tv.Sec), int64(tv.Usec) * 1000
}

// Nano returns the time stored in ts as nanoseconds.
func (ts *Timespec) Nano() int64 {
	return int64(ts.Sec)*1e9 + int64(ts.Nsec)
}

// Nano returns the time stored in tv as nanoseconds.
func (tv *Timeval) Nano() int64 {
	return int64(tv.Sec)*1e9 + int64(tv.Usec)*1000
}

// TimespecToNsec returns the time stored in ts as nanoseconds.
func TimespecToNsec(ts Timespec) int64 { return int64(ts.Sec)*1e9 + int64(ts.Nsec) }

// NsecToTimespec converts a number of nanoseconds into a [Timespec].
func NsecToTimespec(nsec int64) Timespec {
	sec := nsec / 1e9
	nsec = nsec % 1e9
	if nsec < 0 {
		nsec += 1e9
		sec--
	}
	return setTimespec(sec, nsec)
}

// TimevalToNsec returns the time stored in tv as nanoseconds.
func TimevalToNsec(tv Timeval) int64 { return int64(tv.Sec)*1e9 + int64(tv.Usec)*1000 }

// NsecToTimeval converts a number of nanoseconds into a [Timeval].
func NsecToTimeval(nsec int64) Timeval {
	nsec += 999 // round up to microsecond
	usec := nsec % 1e9 / 1e3
	sec := nsec / 1e9
	if usec < 0 {
		usec += 1e6
		sec--
	}
	return setTimeval(sec, usec)
}

// StringByteSlice converts s to a NUL-terminated []byte.
// If s contains a NUL byte, it panics (gc 1.27.1:
// "syscall: string with NUL passed to StringByteSlice").
func StringByteSlice(s string) []byte {
	p := __byteSliceFromString(s)
	if p.err != nil {
		panic("syscall: string with NUL passed to StringByteSlice")
	}
	return p.v
}

// ByteSliceFromString returns a NUL-terminated []byte with the text of
// s. If s contains a NUL byte, it returns (nil, [EINVAL]).
// Multi-result: call sites desugar to __byteSliceFromString via Appendix M
// (staged mrdesugar_m5.go entry); the body below runs only on the gc
// reference leg and is unreachable on graalgo.
func ByteSliceFromString(s string) ([]byte, error) {
	p := __byteSliceFromString(s)
	return p.v, p.err
}

func __byteSliceFromString(s string) bytesErrorPair {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return bytesErrorPair{v: nil, err: EINVAL}
		}
	}
	a := make([]byte, len(s)+1)
	copy(a, s)
	return bytesErrorPair{v: a, err: nil}
}

// StringBytePtr returns a pointer to a NUL-terminated array of bytes.
// If s contains a NUL byte this function panics instead of returning
// an error.
//
// Deprecated: Use [BytePtrFromString] instead.
//
// Fail-fast: the gc body (&StringByteSlice(s)[0]) takes the address of
// an index expression, which is not expressible in frontend/v1 (WS D).
const failStringBytePtr = "syscall.StringBytePtr: no graalgo intrinsic; re-test: address-of on index expr (WS D)"

func StringBytePtr(s string) *byte { panic(failStringBytePtr) }

// BytePtrFromString returns a pointer to a NUL-terminated array of
// bytes containing the text of s. If s contains a NUL byte at any
// location, it returns (nil, [EINVAL]).
func BytePtrFromString(s string) (*byte, error) {
	p := __bytePtrFromString(s)
	return p.p, p.err
}

type bytePtrErrorPair struct {
	p   *byte
	err error
}

func __bytePtrFromString(s string) bytePtrErrorPair {
	p := __byteSliceFromString(s)
	if p.err != nil {
		return bytePtrErrorPair{err: p.err}
	}
	// unsafe.SliceData: &p.v[0] is not expressible in frontend/v1 (WS D).
	return bytePtrErrorPair{p: unsafe.SliceData(p.v)}
}

// entersyscall/exitsyscall are no-ops on graalgo: blocking intrinsics
// run on virtual threads, which unmount the carrier automatically
// (SPEC §3.5 netpoller-deleted precedent). No P is ever released.
func runtime_entersyscall() {}
func runtime_exitsyscall()  {}

// Fork/exec hooks pushed for the runtime's pull linknames. Fork and
// exec do not exist in M8 scope (StartProcess/ForkExec fail-fast), so
// these are no-ops. Re-test: process-spawning milestone.
func runtime_BeforeFork()       {}
func runtime_AfterFork()        {}
func runtime_AfterForkInChild() {}
func runtime_BeforeExec()       {}
func runtime_AfterExec()        {}

//go:linkname graalgoOsGetenvFound graalgo.os.getenvFound
func graalgoOsGetenvFound(key string) int64

//go:linkname graalgoOsGetenv graalgo.os.getenv
func graalgoOsGetenv(key string) string

//go:linkname graalgoOsSetenv graalgo.os.setenv
func graalgoOsSetenv(key, value string) int64

//go:linkname graalgoOsUnsetenv graalgo.os.unsetenv
func graalgoOsUnsetenv(key string) int64

//go:linkname graalgoOsClearenv graalgo.os.clearenv
func graalgoOsClearenv() int64

//go:linkname graalgoOsEnviron graalgo.os.environ
func graalgoOsEnviron() string

//go:linkname graalgoOsGetpid graalgo.os.getpid
func graalgoOsGetpid() int64

//go:linkname graalgoOsGetppid graalgo.os.getppid
func graalgoOsGetppid() int64

//go:linkname graalgoOsGetuid graalgo.os.getuid
func graalgoOsGetuid() int64

//go:linkname graalgoOsGeteuid graalgo.os.geteuid
func graalgoOsGeteuid() int64

//go:linkname graalgoOsGetgid graalgo.os.getgid
func graalgoOsGetgid() int64

//go:linkname graalgoOsGetegid graalgo.os.getegid
func graalgoOsGetegid() int64

//go:linkname graalgoOsGetgroups graalgo.os.getgroups
func graalgoOsGetgroups() string

//go:linkname graalgoOsGetcwd graalgo.os.getcwd
func graalgoOsGetcwd() string

//go:linkname graalgoOsExit graalgo.os.exit
func graalgoOsExit(code int64)

// errnoFromLong converts a graalgo.os intrinsic result (0 ok,
// negative -errno) to an error.
func errnoFromLong(n int64) error {
	if n == 0 {
		return nil
	}
	return Errno(-n)
}

// runtime_envs is pulled by the runtime (gc: //go:linkname in
// runtime). Provided here over the graalgo.os.environ intrinsic
// (NUL-joined "k=v").
func runtime_envs() []string {
	return splitNUL(graalgoOsEnviron())
}

func splitNUL(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if out == nil {
		return []string{}
	}
	return out
}

// Getpagesize returns the OS page size (4096 on linux/amd64;
// differential-pinned vs gc).
func Getpagesize() int { return 4096 }

// Exit causes the current program to exit with the given status code.
// Like gc's, it runs no deferred calls.
func Exit(code int) { graalgoOsExit(int64(code)) }

// runtimeSetenv/runtimeUnsetenv/runtimeClearenv are pulled by the
// runtime. They go through the same GoOs env overlay as
// Setenv/Unsetenv/Clearenv (documented divergence: the real process
// environ is not mutated — invisible to Go code; only native/cgo code
// could observe it, and cgo is excluded). Re-test: Panama FFI.
func runtimeSetenv(k, v string) { _ = graalgoOsSetenv(k, v) }
func runtimeUnsetenv(k string)  { _ = graalgoOsUnsetenv(k) }
func runtimeClearenv(env map[string]int) {
	// No Go-side env copy exists in M8 (the overlay is the source of
	// truth), so env is vestigial.
	_ = env
	_ = graalgoOsClearenv()
}

// Getenv retrieves the value of the environment variable named by key.
// Multi-result: call sites desugar to __getenv via Appendix M (staged
// mrdesugar_m5.go entry); the body below runs only on the gc reference
// leg and is unreachable on graalgo.
func Getenv(key string) (value string, found bool) {
	p := __getenv(key)
	return p.value, p.found
}

type getenvPair struct {
	value string
	found bool
}

func __getenv(key string) getenvPair {
	if len(key) == 0 {
		return getenvPair{}
	}
	if graalgoOsGetenvFound(key) == 0 {
		return getenvPair{}
	}
	return getenvPair{value: graalgoOsGetenv(key), found: true}
}

// Setenv sets the value of the environment variable named by key.
// Key validation mirrors gc (empty/'='/NUL key or NUL in value ->
// EINVAL); the mutation lands in the GoOs overlay.
func Setenv(key, value string) error {
	if len(key) == 0 {
		return EINVAL
	}
	for i := 0; i < len(key); i++ {
		if key[i] == '=' || key[i] == 0 {
			return EINVAL
		}
	}
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return EINVAL
		}
	}
	return errnoFromLong(graalgoOsSetenv(key, value))
}

// Unsetenv unsets a single environment variable.
func Unsetenv(key string) error {
	return errnoFromLong(graalgoOsUnsetenv(key))
}

// Clearenv deletes all environment variables.
func Clearenv() {
	_ = graalgoOsClearenv()
}

// Environ returns a copy of strings representing the environment,
// in the form "key=value".
func Environ() []string {
	return splitNUL(graalgoOsEnviron())
}

// Getpid returns the process id of the caller.
func Getpid() (pid int) { return int(graalgoOsGetpid()) }

// Getppid returns the process id of the caller's parent.
func Getppid() (ppid int) { return int(graalgoOsGetppid()) }

// Getuid returns the numeric user id of the caller.
func Getuid() (uid int) { return int(graalgoOsGetuid()) }

// Geteuid returns the numeric effective user id of the caller.
func Geteuid() (euid int) { return int(graalgoOsGeteuid()) }

// Getgid returns the numeric group id of the caller.
func Getgid() (gid int) { return int(graalgoOsGetgid()) }

// Getegid returns the numeric effective group id of the caller.
func Getegid() (egid int) { return int(graalgoOsGetegid()) }

// Getgroups returns the numeric ids of the caller's groups.
// Multi-result: call sites desugar to __getgroups via Appendix M.
func Getgroups() (gids []int, err error) {
	p := __getgroups()
	return p.gids, p.err
}

type gidsErrorPair struct {
	gids []int
	err  error
}

func __getgroups() gidsErrorPair {
	// graalgo.os.getgroups returns NUL-joined decimal gids.
	s := graalgoOsGetgroups()
	if s == "" {
		return gidsErrorPair{}
	}
	parts := splitNUL(s)
	gids := make([]int, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		r := __parseUint(p)
		if !r.ok {
			return gidsErrorPair{err: EINVAL}
		}
		gids = append(gids, int(r.v))
	}
	return gidsErrorPair{gids: gids}
}

// parseUint parses a decimal string; ok=false on any non-digit.
// Multi-result: the single internal caller (__getgroups) uses
// __parseUint directly; the body below runs only on the gc reference
// leg and is unreachable on graalgo.
func parseUint(s string) (v uint64, ok bool) {
	p := __parseUint(s)
	return p.v, p.ok
}

type uint64BoolPair struct {
	v  uint64
	ok bool
}

func __parseUint(s string) uint64BoolPair {
	if s == "" {
		return uint64BoolPair{}
	}
	var v uint64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return uint64BoolPair{}
		}
		v = v*10 + uint64(c-'0')
	}
	return uint64BoolPair{v: v, ok: true}
}

// Getwd returns the current working directory.
// Multi-result: call sites desugar to __getwd via Appendix M.
// (Shape assumption, flagged: graalgo.os.getcwd returns the path as a
// GoString with no error channel; the intrinsic never returns "" in
// practice — the virtual cwd is always set — so the ENOENT arm is
// defensive only.)
func Getwd() (wd string, err error) {
	p := __getwd()
	return p.v, p.err
}

type stringErrorPair struct {
	v   string
	err error
}

func __getwd() stringErrorPair {
	wd := graalgoOsGetcwd()
	if wd == "" {
		return stringErrorPair{err: ENOENT}
	}
	return stringErrorPair{v: wd}
}

// syscallTrapNames maps trap numbers to SYS_* names for the fail-fast
// messages (generated from zsysnum_linux_amd64.go).
var syscallTrapNames = map[uintptr]string{
	0:   "SYS_READ",
	1:   "SYS_WRITE",
	2:   "SYS_OPEN",
	3:   "SYS_CLOSE",
	4:   "SYS_STAT",
	5:   "SYS_FSTAT",
	6:   "SYS_LSTAT",
	7:   "SYS_POLL",
	8:   "SYS_LSEEK",
	9:   "SYS_MMAP",
	10:  "SYS_MPROTECT",
	11:  "SYS_MUNMAP",
	12:  "SYS_BRK",
	13:  "SYS_RT_SIGACTION",
	14:  "SYS_RT_SIGPROCMASK",
	15:  "SYS_RT_SIGRETURN",
	16:  "SYS_IOCTL",
	17:  "SYS_PREAD64",
	18:  "SYS_PWRITE64",
	19:  "SYS_READV",
	20:  "SYS_WRITEV",
	21:  "SYS_ACCESS",
	22:  "SYS_PIPE",
	23:  "SYS_SELECT",
	24:  "SYS_SCHED_YIELD",
	25:  "SYS_MREMAP",
	26:  "SYS_MSYNC",
	27:  "SYS_MINCORE",
	28:  "SYS_MADVISE",
	29:  "SYS_SHMGET",
	30:  "SYS_SHMAT",
	31:  "SYS_SHMCTL",
	32:  "SYS_DUP",
	33:  "SYS_DUP2",
	34:  "SYS_PAUSE",
	35:  "SYS_NANOSLEEP",
	36:  "SYS_GETITIMER",
	37:  "SYS_ALARM",
	38:  "SYS_SETITIMER",
	39:  "SYS_GETPID",
	40:  "SYS_SENDFILE",
	41:  "SYS_SOCKET",
	42:  "SYS_CONNECT",
	43:  "SYS_ACCEPT",
	44:  "SYS_SENDTO",
	45:  "SYS_RECVFROM",
	46:  "SYS_SENDMSG",
	47:  "SYS_RECVMSG",
	48:  "SYS_SHUTDOWN",
	49:  "SYS_BIND",
	50:  "SYS_LISTEN",
	51:  "SYS_GETSOCKNAME",
	52:  "SYS_GETPEERNAME",
	53:  "SYS_SOCKETPAIR",
	54:  "SYS_SETSOCKOPT",
	55:  "SYS_GETSOCKOPT",
	56:  "SYS_CLONE",
	57:  "SYS_FORK",
	58:  "SYS_VFORK",
	59:  "SYS_EXECVE",
	60:  "SYS_EXIT",
	61:  "SYS_WAIT4",
	62:  "SYS_KILL",
	63:  "SYS_UNAME",
	64:  "SYS_SEMGET",
	65:  "SYS_SEMOP",
	66:  "SYS_SEMCTL",
	67:  "SYS_SHMDT",
	68:  "SYS_MSGGET",
	69:  "SYS_MSGSND",
	70:  "SYS_MSGRCV",
	71:  "SYS_MSGCTL",
	72:  "SYS_FCNTL",
	73:  "SYS_FLOCK",
	74:  "SYS_FSYNC",
	75:  "SYS_FDATASYNC",
	76:  "SYS_TRUNCATE",
	77:  "SYS_FTRUNCATE",
	78:  "SYS_GETDENTS",
	79:  "SYS_GETCWD",
	80:  "SYS_CHDIR",
	81:  "SYS_FCHDIR",
	82:  "SYS_RENAME",
	83:  "SYS_MKDIR",
	84:  "SYS_RMDIR",
	85:  "SYS_CREAT",
	86:  "SYS_LINK",
	87:  "SYS_UNLINK",
	88:  "SYS_SYMLINK",
	89:  "SYS_READLINK",
	90:  "SYS_CHMOD",
	91:  "SYS_FCHMOD",
	92:  "SYS_CHOWN",
	93:  "SYS_FCHOWN",
	94:  "SYS_LCHOWN",
	95:  "SYS_UMASK",
	96:  "SYS_GETTIMEOFDAY",
	97:  "SYS_GETRLIMIT",
	98:  "SYS_GETRUSAGE",
	99:  "SYS_SYSINFO",
	100: "SYS_TIMES",
	101: "SYS_PTRACE",
	102: "SYS_GETUID",
	103: "SYS_SYSLOG",
	104: "SYS_GETGID",
	105: "SYS_SETUID",
	106: "SYS_SETGID",
	107: "SYS_GETEUID",
	108: "SYS_GETEGID",
	109: "SYS_SETPGID",
	110: "SYS_GETPPID",
	111: "SYS_GETPGRP",
	112: "SYS_SETSID",
	113: "SYS_SETREUID",
	114: "SYS_SETREGID",
	115: "SYS_GETGROUPS",
	116: "SYS_SETGROUPS",
	117: "SYS_SETRESUID",
	118: "SYS_GETRESUID",
	119: "SYS_SETRESGID",
	120: "SYS_GETRESGID",
	121: "SYS_GETPGID",
	122: "SYS_SETFSUID",
	123: "SYS_SETFSGID",
	124: "SYS_GETSID",
	125: "SYS_CAPGET",
	126: "SYS_CAPSET",
	127: "SYS_RT_SIGPENDING",
	128: "SYS_RT_SIGTIMEDWAIT",
	129: "SYS_RT_SIGQUEUEINFO",
	130: "SYS_RT_SIGSUSPEND",
	131: "SYS_SIGALTSTACK",
	132: "SYS_UTIME",
	133: "SYS_MKNOD",
	134: "SYS_USELIB",
	135: "SYS_PERSONALITY",
	136: "SYS_USTAT",
	137: "SYS_STATFS",
	138: "SYS_FSTATFS",
	139: "SYS_SYSFS",
	140: "SYS_GETPRIORITY",
	141: "SYS_SETPRIORITY",
	142: "SYS_SCHED_SETPARAM",
	143: "SYS_SCHED_GETPARAM",
	144: "SYS_SCHED_SETSCHEDULER",
	145: "SYS_SCHED_GETSCHEDULER",
	146: "SYS_SCHED_GET_PRIORITY_MAX",
	147: "SYS_SCHED_GET_PRIORITY_MIN",
	148: "SYS_SCHED_RR_GET_INTERVAL",
	149: "SYS_MLOCK",
	150: "SYS_MUNLOCK",
	151: "SYS_MLOCKALL",
	152: "SYS_MUNLOCKALL",
	153: "SYS_VHANGUP",
	154: "SYS_MODIFY_LDT",
	155: "SYS_PIVOT_ROOT",
	156: "SYS__SYSCTL",
	157: "SYS_PRCTL",
	158: "SYS_ARCH_PRCTL",
	159: "SYS_ADJTIMEX",
	160: "SYS_SETRLIMIT",
	161: "SYS_CHROOT",
	162: "SYS_SYNC",
	163: "SYS_ACCT",
	164: "SYS_SETTIMEOFDAY",
	165: "SYS_MOUNT",
	166: "SYS_UMOUNT2",
	167: "SYS_SWAPON",
	168: "SYS_SWAPOFF",
	169: "SYS_REBOOT",
	170: "SYS_SETHOSTNAME",
	171: "SYS_SETDOMAINNAME",
	172: "SYS_IOPL",
	173: "SYS_IOPERM",
	174: "SYS_CREATE_MODULE",
	175: "SYS_INIT_MODULE",
	176: "SYS_DELETE_MODULE",
	177: "SYS_GET_KERNEL_SYMS",
	178: "SYS_QUERY_MODULE",
	179: "SYS_QUOTACTL",
	180: "SYS_NFSSERVCTL",
	181: "SYS_GETPMSG",
	182: "SYS_PUTPMSG",
	183: "SYS_AFS_SYSCALL",
	184: "SYS_TUXCALL",
	185: "SYS_SECURITY",
	186: "SYS_GETTID",
	187: "SYS_READAHEAD",
	188: "SYS_SETXATTR",
	189: "SYS_LSETXATTR",
	190: "SYS_FSETXATTR",
	191: "SYS_GETXATTR",
	192: "SYS_LGETXATTR",
	193: "SYS_FGETXATTR",
	194: "SYS_LISTXATTR",
	195: "SYS_LLISTXATTR",
	196: "SYS_FLISTXATTR",
	197: "SYS_REMOVEXATTR",
	198: "SYS_LREMOVEXATTR",
	199: "SYS_FREMOVEXATTR",
	200: "SYS_TKILL",
	201: "SYS_TIME",
	202: "SYS_FUTEX",
	203: "SYS_SCHED_SETAFFINITY",
	204: "SYS_SCHED_GETAFFINITY",
	205: "SYS_SET_THREAD_AREA",
	206: "SYS_IO_SETUP",
	207: "SYS_IO_DESTROY",
	208: "SYS_IO_GETEVENTS",
	209: "SYS_IO_SUBMIT",
	210: "SYS_IO_CANCEL",
	211: "SYS_GET_THREAD_AREA",
	212: "SYS_LOOKUP_DCOOKIE",
	213: "SYS_EPOLL_CREATE",
	214: "SYS_EPOLL_CTL_OLD",
	215: "SYS_EPOLL_WAIT_OLD",
	216: "SYS_REMAP_FILE_PAGES",
	217: "SYS_GETDENTS64",
	218: "SYS_SET_TID_ADDRESS",
	219: "SYS_RESTART_SYSCALL",
	220: "SYS_SEMTIMEDOP",
	221: "SYS_FADVISE64",
	222: "SYS_TIMER_CREATE",
	223: "SYS_TIMER_SETTIME",
	224: "SYS_TIMER_GETTIME",
	225: "SYS_TIMER_GETOVERRUN",
	226: "SYS_TIMER_DELETE",
	227: "SYS_CLOCK_SETTIME",
	228: "SYS_CLOCK_GETTIME",
	229: "SYS_CLOCK_GETRES",
	230: "SYS_CLOCK_NANOSLEEP",
	231: "SYS_EXIT_GROUP",
	232: "SYS_EPOLL_WAIT",
	233: "SYS_EPOLL_CTL",
	234: "SYS_TGKILL",
	235: "SYS_UTIMES",
	236: "SYS_VSERVER",
	237: "SYS_MBIND",
	238: "SYS_SET_MEMPOLICY",
	239: "SYS_GET_MEMPOLICY",
	240: "SYS_MQ_OPEN",
	241: "SYS_MQ_UNLINK",
	242: "SYS_MQ_TIMEDSEND",
	243: "SYS_MQ_TIMEDRECEIVE",
	244: "SYS_MQ_NOTIFY",
	245: "SYS_MQ_GETSETATTR",
	246: "SYS_KEXEC_LOAD",
	247: "SYS_WAITID",
	248: "SYS_ADD_KEY",
	249: "SYS_REQUEST_KEY",
	250: "SYS_KEYCTL",
	251: "SYS_IOPRIO_SET",
	252: "SYS_IOPRIO_GET",
	253: "SYS_INOTIFY_INIT",
	254: "SYS_INOTIFY_ADD_WATCH",
	255: "SYS_INOTIFY_RM_WATCH",
	256: "SYS_MIGRATE_PAGES",
	257: "SYS_OPENAT",
	258: "SYS_MKDIRAT",
	259: "SYS_MKNODAT",
	260: "SYS_FCHOWNAT",
	261: "SYS_FUTIMESAT",
	262: "SYS_NEWFSTATAT",
	263: "SYS_UNLINKAT",
	264: "SYS_RENAMEAT",
	265: "SYS_LINKAT",
	266: "SYS_SYMLINKAT",
	267: "SYS_READLINKAT",
	268: "SYS_FCHMODAT",
	269: "SYS_FACCESSAT",
	270: "SYS_PSELECT6",
	271: "SYS_PPOLL",
	272: "SYS_UNSHARE",
	273: "SYS_SET_ROBUST_LIST",
	274: "SYS_GET_ROBUST_LIST",
	275: "SYS_SPLICE",
	276: "SYS_TEE",
	277: "SYS_SYNC_FILE_RANGE",
	278: "SYS_VMSPLICE",
	279: "SYS_MOVE_PAGES",
	280: "SYS_UTIMENSAT",
	281: "SYS_EPOLL_PWAIT",
	282: "SYS_SIGNALFD",
	283: "SYS_TIMERFD_CREATE",
	284: "SYS_EVENTFD",
	285: "SYS_FALLOCATE",
	286: "SYS_TIMERFD_SETTIME",
	287: "SYS_TIMERFD_GETTIME",
	288: "SYS_ACCEPT4",
	289: "SYS_SIGNALFD4",
	290: "SYS_EVENTFD2",
	291: "SYS_EPOLL_CREATE1",
	292: "SYS_DUP3",
	293: "SYS_PIPE2",
	294: "SYS_INOTIFY_INIT1",
	295: "SYS_PREADV",
	296: "SYS_PWRITEV",
	297: "SYS_RT_TGSIGQUEUEINFO",
	298: "SYS_PERF_EVENT_OPEN",
	299: "SYS_RECVMMSG",
	300: "SYS_FANOTIFY_INIT",
	301: "SYS_FANOTIFY_MARK",
	302: "SYS_PRLIMIT64",
}

func syscallTrapMsg(name string, trap uintptr) string {
	num := strconv.FormatUint(uint64(trap), 10)
	sname, ok := syscallTrapNames[trap]
	if !ok {
		sname = "unknown"
	}
	return "syscall." + name + ": trap " + num + " (" + sname + ") has no graalgo intrinsic; re-test: Panama FFI (Phase 4)"
}

// Syscall/RawSyscall/Syscall6/RawSyscall6 with arbitrary trap numbers
// are inexpressible under the handle-table value model (SPEC §7.3: a
// uintptr arriving at a trap site is a handle token, not a native
// address; no generic dispatcher can marshal pointer args without
// per-trap type knowledge). They fail fast with the trap number and
// name. Multi-result: call sites desugar via Appendix M; the public
// bodies below run only on the gc reference leg.
func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("Syscall", trap))
}

func __syscall(trap, a1, a2, a3 uintptr) syscallTriple {
	panic(syscallTrapMsg("Syscall", trap))
}

func RawSyscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("RawSyscall", trap))
}

func __rawsyscall(trap, a1, a2, a3 uintptr) syscallTriple {
	panic(syscallTrapMsg("RawSyscall", trap))
}

func Syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("Syscall6", trap))
}

func __syscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) syscallTriple {
	panic(syscallTrapMsg("Syscall6", trap))
}

func RawSyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err Errno) {
	panic(syscallTrapMsg("RawSyscall6", trap))
}

func __rawsyscall6(trap, a1, a2, a3, a4, a5, a6 uintptr) syscallTriple {
	panic(syscallTrapMsg("RawSyscall6", trap))
}

type syscallTriple struct {
	r1  uintptr
	r2  uintptr
	err Errno
}

type intErrorPair struct {
	v   int
	err error
}

type int64ErrorPair struct {
	v   int64
	err error
}

type bytesErrorPair struct {
	v   []byte
	err error
}

type startProcessTriple struct {
	pid    int
	handle uintptr
	err    error
}

type acceptTriple struct {
	fd  int
	sa  Sockaddr
	err error
}

type recvmsgTuple struct {
	n         int
	oobn      int
	recvflags int
	from      Sockaddr
	err       error
}

const failOpen = "syscall.Open: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Open(path string, mode int, perm uint32) (fd int, err error) {
	panic(failOpen)
}

func __open(path string, mode int, perm uint32) intErrorPair {
	panic(failOpen)
}

const failRead = "syscall.Read: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Read(fd int, p []byte) (n int, err error) {
	panic(failRead)
}

func __read(fd int, p []byte) intErrorPair {
	panic(failRead)
}

const failWrite = "syscall.Write: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Write(fd int, p []byte) (n int, err error) {
	panic(failWrite)
}

func __write(fd int, p []byte) intErrorPair {
	panic(failWrite)
}

const failPread = "syscall.Pread: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Pread(fd int, p []byte, offset int64) (n int, err error) {
	panic(failPread)
}

func __pread(fd int, p []byte, offset int64) intErrorPair {
	panic(failPread)
}

const failPwrite = "syscall.Pwrite: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Pwrite(fd int, p []byte, offset int64) (n int, err error) {
	panic(failPwrite)
}

func __pwrite(fd int, p []byte, offset int64) intErrorPair {
	panic(failPwrite)
}

const failSeek = "syscall.Seek: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Seek(fd int, offset int64, whence int) (off int64, err error) {
	panic(failSeek)
}

func __seek(fd int, offset int64, whence int) int64ErrorPair {
	panic(failSeek)
}

const failReadlink = "syscall.Readlink: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Readlink(path string, buf []byte) (n int, err error) {
	panic(failReadlink)
}

func __readlink(path string, buf []byte) intErrorPair {
	panic(failReadlink)
}

const failMmap = "syscall.Mmap: no graalgo intrinsic; re-test: first real consumer"

func Mmap(fd int, offset int64, length int, prot int, flags int) (data []byte, err error) {
	panic(failMmap)
}

func __mmap(fd int, offset int64, length int, prot int, flags int) bytesErrorPair {
	panic(failMmap)
}

const failStartProcess = "syscall.StartProcess: no graalgo intrinsic; re-test: os/exec milestone"

func StartProcess(argv0 string, argv []string, attr *ProcAttr) (pid int, handle uintptr, err error) {
	panic(failStartProcess)
}

func __startprocess(argv0 string, argv []string, attr *ProcAttr) startProcessTriple {
	panic(failStartProcess)
}

const failForkExec = "syscall.ForkExec: no graalgo intrinsic; re-test: os/exec milestone"

func ForkExec(argv0 string, argv []string, attr *ProcAttr) (pid int, err error) {
	panic(failForkExec)
}

func __forkexec(argv0 string, argv []string, attr *ProcAttr) intErrorPair {
	panic(failForkExec)
}

const failWait4 = "syscall.Wait4: no graalgo intrinsic; re-test: os/exec milestone"

func Wait4(pid int, wstatus *WaitStatus, options int, rusage *Rusage) (wpid int, err error) {
	panic(failWait4)
}

func __wait4(pid int, wstatus *WaitStatus, options int, rusage *Rusage) intErrorPair {
	panic(failWait4)
}

const failSocket = "syscall.Socket: no graalgo intrinsic; re-test: net package bring-up"

func Socket(domain, typ, proto int) (fd int, err error) {
	panic(failSocket)
}

func __socket(domain, typ, proto int) intErrorPair {
	panic(failSocket)
}

const failAccept = "syscall.Accept: no graalgo intrinsic; re-test: net package bring-up"

func Accept(fd int) (nfd int, sa Sockaddr, err error) {
	panic(failAccept)
}

func __accept(fd int) acceptTriple {
	panic(failAccept)
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Accept4 is required by internal/poll, which is required by the real net.
const failAccept4 = "syscall.Accept4: temporary until ASM backend (his 2026-10-15 order)"

func Accept4(fd int, flags int) (nfd int, sa Sockaddr, err error) {
	panic(failAccept4)
}

const failGetsockoptInt = "syscall.GetsockoptInt: no graalgo intrinsic; re-test: net package bring-up"

func GetsockoptInt(fd, level, opt int) (value int, err error) {
	panic(failGetsockoptInt)
}

func __getsockoptint(fd, level, opt int) intErrorPair {
	panic(failGetsockoptInt)
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Getsockname/Getpeername required by the real net package (fd_unix.go).
const failGetsockname = "syscall.Getsockname: temporary until ASM backend (his 2026-10-15 order)"

func Getsockname(fd int) (sa Sockaddr, err error) {
	panic(failGetsockname)
}

const failGetpeername = "syscall.Getpeername: temporary until ASM backend (his 2026-10-15 order)"

func Getpeername(fd int) (sa Sockaddr, err error) {
	panic(failGetpeername)
}

const failRecvmsg = "syscall.Recvmsg: no graalgo intrinsic; re-test: net package bring-up"

func Recvmsg(fd int, p, oob []byte, flags int) (n int, oobn int, recvflags int, from Sockaddr, err error) {
	panic(failRecvmsg)
}

func __recvmsg(fd int, p, oob []byte, flags int) recvmsgTuple {
	panic(failRecvmsg)
}

const failEpollCreate = "syscall.EpollCreate: no graalgo intrinsic; re-test: per-consumer demand"

func EpollCreate(size int) (fd int, err error) {
	panic(failEpollCreate)
}

func __epollcreate(size int) intErrorPair {
	panic(failEpollCreate)
}

const failEpollCreate1 = "syscall.EpollCreate1: no graalgo intrinsic; re-test: per-consumer demand"

func EpollCreate1(flag int) (fd int, err error) {
	panic(failEpollCreate1)
}

func __epollcreate1(flag int) intErrorPair {
	panic(failEpollCreate1)
}

const failEpollWait = "syscall.EpollWait: no graalgo intrinsic; re-test: per-consumer demand"

func EpollWait(epfd int, events []EpollEvent, msec int) (n int, err error) {
	panic(failEpollWait)
}

func __epollwait(epfd int, events []EpollEvent, msec int) intErrorPair {
	panic(failEpollWait)
}

const failInotifyInit = "syscall.InotifyInit: no graalgo intrinsic; re-test: per-consumer demand"

func InotifyInit() (fd int, err error) {
	panic(failInotifyInit)
}

func __inotifyinit() intErrorPair {
	panic(failInotifyInit)
}

const failInotifyAddWatch = "syscall.InotifyAddWatch: no graalgo intrinsic; re-test: per-consumer demand"

func InotifyAddWatch(fd int, pathname string, mask uint32) (watchdesc int, err error) {
	panic(failInotifyAddWatch)
}

func __inotifyaddwatch(fd int, pathname string, mask uint32) intErrorPair {
	panic(failInotifyAddWatch)
}

const failInotifyRmWatch = "syscall.InotifyRmWatch: no graalgo intrinsic; re-test: per-consumer demand"

func InotifyRmWatch(fd int, watchdesc uint32) (success int, err error) {
	panic(failInotifyRmWatch)
}

func __inotifyrmwatch(fd int, watchdesc uint32) intErrorPair {
	panic(failInotifyRmWatch)
}

const failClose = "syscall.Close: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Close(fd int) (err error) {
	panic(failClose)
}

const failStat = "syscall.Stat: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Stat(path string, stat *Stat_t) (err error) {
	panic(failStat)
}

const failLstat = "syscall.Lstat: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Lstat(path string, stat *Stat_t) (err error) {
	panic(failLstat)
}

const failFstat = "syscall.Fstat: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Fstat(fd int, stat *Stat_t) (err error) {
	panic(failFstat)
}

const failMkdir = "syscall.Mkdir: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Mkdir(path string, mode uint32) (err error) {
	panic(failMkdir)
}

const failChdir = "syscall.Chdir: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Chdir(path string) (err error) {
	panic(failChdir)
}

const failChmod = "syscall.Chmod: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Chmod(path string, mode uint32) (err error) {
	panic(failChmod)
}

// TEMPORARY: synthetic until ASM backend (his 2026-10-15 order).
// Faccessat/Fchmodat/Fstatat/Openat are required by internal/syscall/unix
// (used by net). The real net only takes Faccessat's function value at
// package init; actual calls go through paths not in net's basic Dial/Listen.
const failFaccessat = "syscall.Faccessat: temporary until ASM backend (his 2026-10-15 order)"

func Faccessat(dirfd int, path string, mode uint32, flags int) (err error) {
	panic(failFaccessat)
}

const failFchmodat = "syscall.Fchmodat: temporary until ASM backend (his 2026-10-15 order)"

func Fchmodat(dirfd int, path string, mode uint32, flags int) error {
	panic(failFchmodat)
}

const failFstatat = "syscall.Fstatat: temporary until ASM backend (his 2026-10-15 order)"

func Fstatat(fd int, path string, stat *Stat_t, flags int) error {
	panic(failFstatat)
}

const failOpenat = "syscall.Openat: temporary until ASM backend (his 2026-10-15 order)"

func Openat(dirfd int, path string, flags int, mode uint32) (fd int, err error) {
	panic(failOpenat)
}

const failChown = "syscall.Chown: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Chown(path string, uid int, gid int) (err error) {
	panic(failChown)
}

const failLchown = "syscall.Lchown: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Lchown(path string, uid int, gid int) (err error) {
	panic(failLchown)
}

const failLink = "syscall.Link: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Link(oldpath string, newpath string) (err error) {
	panic(failLink)
}

const failSymlink = "syscall.Symlink: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Symlink(oldpath string, newpath string) (err error) {
	panic(failSymlink)
}

const failRename = "syscall.Rename: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Rename(oldpath string, newpath string) (err error) {
	panic(failRename)
}

const failRmdir = "syscall.Rmdir: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Rmdir(path string) error {
	panic(failRmdir)
}

const failUnlink = "syscall.Unlink: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Unlink(path string) error {
	panic(failUnlink)
}

const failTruncate = "syscall.Truncate: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Truncate(path string, length int64) (err error) {
	panic(failTruncate)
}

const failUtimesNano = "syscall.UtimesNano: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func UtimesNano(path string, ts []Timespec) error {
	panic(failUtimesNano)
}

const failUname = "syscall.Uname: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Uname(buf *Utsname) (err error) {
	panic(failUname)
}

const failKill = "syscall.Kill: no graalgo intrinsic; re-test: os/exec milestone"

func Kill(pid int, sig Signal) (err error) {
	panic(failKill)
}

const failSetNonblock = "syscall.SetNonblock: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func SetNonblock(fd int, nonblocking bool) error {
	panic(failSetNonblock)
}

const failCloseOnExec = "syscall.CloseOnExec: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func CloseOnExec(fd int) {
	panic(failCloseOnExec)
}

const failPipe2 = "syscall.Pipe2: no graalgo intrinsic; use os; re-test: fd-domain unification, Phase 4/Panama"

func Pipe2(p []int, flags int) error {
	panic(failPipe2)
}

const failMunmap = "syscall.Munmap: no graalgo intrinsic; re-test: first real consumer"

func Munmap(b []byte) (err error) {
	panic(failMunmap)
}

const failMprotect = "syscall.Mprotect: no graalgo intrinsic; re-test: first real consumer"

func Mprotect(b []byte, prot int) (err error) {
	panic(failMprotect)
}

const failMadvise = "syscall.Madvise: no graalgo intrinsic; re-test: first real consumer"

func Madvise(b []byte, advice int) (err error) {
	panic(failMadvise)
}

const failFlock = "syscall.Flock: no graalgo intrinsic; re-test: per-consumer demand"

func Flock(fd int, how int) (err error) {
	panic(failFlock)
}

const failGetrlimit = "syscall.Getrlimit: no graalgo intrinsic; re-test: per-consumer demand"

func Getrlimit(resource int, rlim *Rlimit) (err error) {
	panic(failGetrlimit)
}

const failSetrlimit = "syscall.Setrlimit: no graalgo intrinsic; re-test: per-consumer demand"

func Setrlimit(resource int, rlim *Rlimit) error {
	panic(failSetrlimit)
}

const failSetuid = "syscall.Setuid: no graalgo intrinsic; re-test: per-consumer demand"

func Setuid(uid int) (err error) {
	panic(failSetuid)
}

const failSetgid = "syscall.Setgid: no graalgo intrinsic; re-test: per-consumer demand"

func Setgid(gid int) (err error) {
	panic(failSetgid)
}

const failBind = "syscall.Bind: no graalgo intrinsic; re-test: net package bring-up"

func Bind(fd int, sa Sockaddr) (err error) {
	panic(failBind)
}

const failConnect = "syscall.Connect: no graalgo intrinsic; re-test: net package bring-up"

func Connect(fd int, sa Sockaddr) (err error) {
	panic(failConnect)
}

const failListen = "syscall.Listen: no graalgo intrinsic; re-test: net package bring-up"

func Listen(s int, n int) (err error) {
	panic(failListen)
}

const failSetsockoptInt = "syscall.SetsockoptInt: no graalgo intrinsic; re-test: net package bring-up"

func SetsockoptInt(fd, level, opt int, value int) (err error) {
	panic(failSetsockoptInt)
}

const failSendmsg = "syscall.Sendmsg: no graalgo intrinsic; re-test: net package bring-up"

func Sendmsg(fd int, p, oob []byte, to Sockaddr, flags int) (err error) {
	panic(failSendmsg)
}

const failShutdown = "syscall.Shutdown: no graalgo intrinsic; re-test: net package bring-up"

func Shutdown(fd int, how int) (err error) {
	panic(failShutdown)
}

const failEpollCtl = "syscall.EpollCtl: no graalgo intrinsic; re-test: per-consumer demand"

func EpollCtl(epfd int, op int, fd int, event *EpollEvent) (err error) {
	panic(failEpollCtl)
}
`
