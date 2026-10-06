package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Deploy-ryanl-in/personal-paas/internal/store"
	"strings"
)

// Remove only explicitly platform-labelled images outside retained releases.
// Never use daemon-wide prune, and never force removal of an image in use.
func (d *Docker) PruneImages(ctx context.Context, r store.Release, retained []store.Release) error {
	keep := map[string]bool{}
	for _, release := range retained {
		for _, ref := range release.Images {
			keep[ref] = true
		}
	}
	data, err := d.Run(ctx, "image", "ls", "--no-trunc", "--quiet", "--filter", "label="+Managed, "--filter", fmt.Sprintf("label=in.ryanl.paas.repository=%d", r.Repo.ID))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range strings.Fields(string(data)) {
		if seen[id] {
			continue
		}
		seen[id] = true
		raw, err := d.Run(ctx, "image", "inspect", id)
		if err != nil {
			continue
		}
		var images []struct {
			RepoTags    []string
			RepoDigests []string
			Config      struct{ Labels map[string]string }
		}
		if json.Unmarshal(raw, &images) != nil || len(images) != 1 {
			continue
		}
		image := images[0]
		used, err := d.Run(ctx, "ps", "-a", "--quiet", "--filter", "ancestor="+id)
		if err != nil {
			return err
		}
		if len(strings.TrimSpace(string(used))) > 0 {
			continue
		}
		if image.Config.Labels["org.opencontainers.image.source"] != "https://github.com/"+r.Repo.FullName {
			continue
		}
		prefix := fmt.Sprintf("ghcr.io/%s/paas-%d-", strings.ToLower(r.Repo.Owner), r.Repo.ID)
		safe := true
		for _, ref := range append(image.RepoTags, image.RepoDigests...) {
			if !strings.HasPrefix(ref, prefix) || keep[ref] {
				safe = false
			}
		}
		if !safe {
			continue
		}
		refs := append(append([]string{}, image.RepoTags...), image.RepoDigests...)
		if len(refs) == 0 {
			refs = []string{id}
		}
		if _, err = d.Run(ctx, append([]string{"image", "rm"}, refs...)...); err != nil {
			return err
		}
	}
	return nil
}
