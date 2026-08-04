package skilltree

import (
	"strings"
	"testing"
)

func TestHashIsCanonicalAndSensitiveToEveryOwnedField(t *testing.T) {
	t.Parallel()
	regular := File{Path: "b.txt", Data: []byte("content")}
	executable := File{Path: "a.sh", Data: []byte("content"), Executable: true}

	baseline := Hash([]File{regular, executable})
	if !strings.HasPrefix(baseline, HashPrefix) {
		t.Fatalf("hash %q does not carry the version prefix", baseline)
	}
	if reordered := Hash([]File{executable, regular}); reordered != baseline {
		t.Fatalf("input order changed a canonical tree hash: %q != %q", reordered, baseline)
	}

	cases := map[string][]File{
		"path":       {{Path: "c.txt", Data: regular.Data}, executable},
		"content":    {{Path: regular.Path, Data: []byte("different")}, executable},
		"executable": {{Path: regular.Path, Data: regular.Data, Executable: true}, executable},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Hash(files); got == baseline {
				t.Fatalf("changing %s did not change the tree hash", name)
			}
		})
	}
}
