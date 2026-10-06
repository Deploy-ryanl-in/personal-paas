package runtime

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Deploy-ryanl-in/personal-paas/internal/manifest"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
)

// Docker 29's containerd store may report a digest in both RepoTags and
// RepoDigests. Deleting both copies makes Docker fail after the first removal.
func TestCleanupDeduplicatesDockerDigestAndProtectsInUseImages(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
shift 4
case "$1 $2" in
 "image ls") printf 'unused\nused\nforeign\n';;
 "image inspect")
  case "$3" in
   unused) printf '[{"RepoTags":["ghcr.io/owner/paas-42-web@sha256:abc"],"RepoDigests":["ghcr.io/owner/paas-42-web@sha256:abc"],"Config":{"Labels":{"org.opencontainers.image.source":"https://github.com/owner/app"}}}]';;
   used) printf '[{"RepoTags":[],"RepoDigests":["ghcr.io/owner/paas-42-web@sha256:def"],"Config":{"Labels":{"org.opencontainers.image.source":"https://github.com/owner/app"}}}]';;
   foreign) printf '[{"RepoTags":[],"RepoDigests":["ghcr.io/other/paas-41-web@sha256:ghi"],"Config":{"Labels":{"org.opencontainers.image.source":"https://github.com/other/app"}}}]';;
  esac;;
 "ps -a") case "$5" in ancestor=used) printf 'stopped-container\n';; esac;;
 "image rm")
  [ "$#" = 3 ] || exit 7
  [ "$3" = 'ghcr.io/owner/paas-42-web@sha256:abc' ] || exit 8
  printf '%s\n' "$3" >> "$PAAS_REMOVAL_LOG";;
 *) exit 9;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "removed")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PAAS_REMOVAL_LOG", log)
	d := Docker{Socket: "unix:///test.sock", RegistryConfig: dir}
	r := store.Release{Repo: manifest.Identity{ID: 42, Owner: "owner", FullName: "owner/app"}}
	if err := d.PruneImages(context.Background(), r, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != "ghcr.io/owner/paas-42-web@sha256:abc" {
		t.Fatal("unsafe removal", string(data))
	}
	if err := os.Remove(log); err != nil {
		t.Fatal(err)
	}
	r.Images = map[string]string{"web": "ghcr.io/owner/paas-42-web@sha256:abc"}
	if err := d.PruneImages(context.Background(), r, []store.Release{r}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("retained release image was deleted")
	}
}
