package index

import "pault.ag/go/debian/version"

type Source struct {
	Package string
	Version version.Version
}

type Index map[string]Source

// DSC contains the URL to a DSC and the total file size of the DSC plus all
// files it references.
type DSC struct {
	URL  string
	Size int64
}

type URIs map[Source]DSC

// BlockLocation describes the location (including the size) of a same-length
// block within an index file.
type BlockLocation struct {
	BlockOffset uint32
	BlockLength uint32
}
