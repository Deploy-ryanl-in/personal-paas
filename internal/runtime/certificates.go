package runtime

import (
	"context"
	"os"
	"time"
)

func certificateChanges(ctx context.Context, changes chan<- struct{}) {
	var last time.Time
	for {
		if st, err := os.Stat("/var/lib/paas-acme/certificate-renewed"); err == nil && st.ModTime() != last {
			if !last.IsZero() {
				select {
				case changes <- struct{}{}:
				default:
				}
			}
			last = st.ModTime()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
	}
}
