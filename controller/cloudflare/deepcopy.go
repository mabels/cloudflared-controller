package cloudflare

import "encoding/json"

// deepCopyRaw copies a cloudflare-go response struct by decoding its raw
// json again; a plain struct copy would share slices and maps.
func deepCopyRaw[T any, PT interface {
	*T
	json.Unmarshaler
}](in T, raw string) T {
	if raw == "" {
		return in
	}
	var out T
	if err := PT(&out).UnmarshalJSON([]byte(raw)); err != nil {
		return in
	}
	return out
}
