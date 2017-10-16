package main

import "pault.ag/go/debian/version"

func mustParseVersion(s string) version.Version {
	v, err := version.Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}
