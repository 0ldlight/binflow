//go:build windows

package httpapi

// filestoreSpace returns (total, free) bytes of the volume hosting the
// filestore. The windows build reports zeros — the info face renders the
// honest "0" fields rather than a volume probe that the port never grew;
// the unix split (storage_info_unix.go) carries the real statfs.
func filestoreSpace(_ string) (int64, int64) { return 0, 0 }
