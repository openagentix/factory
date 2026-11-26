package repo

// Limits bound the size of an export or an import. A zero or negative field
// means the default.
type Limits struct {
	MaxTotalBytes int64 // bytes in all files
	MaxFileBytes  int64 // bytes in one file
	MaxEntries    int   // entries (files, links and directories)
	MaxPathBytes  int   // bytes in one path, used by Import
	MaxDepth      int   // directory levels, used by Import
}

// Default limits.
const (
	DefaultMaxTotalBytes = 2 << 30
	DefaultMaxFileBytes  = 200 << 20
	DefaultMaxEntries    = 200000
	DefaultMaxPathBytes  = 4096
	DefaultMaxDepth      = 64
)

// WithDefaults returns l with every unset field set to its default.
func (l Limits) WithDefaults() Limits {
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultMaxFileBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultMaxEntries
	}
	if l.MaxPathBytes <= 0 {
		l.MaxPathBytes = DefaultMaxPathBytes
	}
	if l.MaxDepth <= 0 {
		l.MaxDepth = DefaultMaxDepth
	}
	return l
}
