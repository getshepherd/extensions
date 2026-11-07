package paddock

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
)

// Solar's Linear icons, the set Shepherd draws with: name → the inside of a 24×24 SVG.
//
//go:embed icons/solar-linear.json
var solarJSON []byte

var solar = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	if err := json.Unmarshal(solarJSON, &m); err != nil {
		panic(err)
	}
	return m
})

// icon serves /_paddock/icons/<name>.svg; <p-icon name> shows it as a mask, so it takes the text's color.
func icon(w http.ResponseWriter, r *http.Request) {
	body, ok := solar()[strings.TrimSuffix(r.PathValue("name"), ".svg")]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "max-age=86400")
	fmt.Fprintf(w, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">%s</svg>`, body)
}

// iconNames serves /_paddock/icons.json, the names, sorted: for a picker, or for finding one.
func iconNames(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, slices.Sorted(maps.Keys(solar())))
}
