package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"time"
)

func (d *Docker) Events(ctx context.Context, changes chan<- struct{}) {
	for {
		if ctx.Err() != nil {
			return
		}
		cmd := d.Command(ctx, "events", "--filter", "type=container", "--filter", "label="+Managed, "--format", "{{json .}}")
		stdout, err := cmd.StdoutPipe()
		if err == nil {
			err = cmd.Start()
		}
		if err == nil {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				var event struct{ Action string }
				if json.Unmarshal(scanner.Bytes(), &event) != nil {
					continue
				}
				switch event.Action {
				case "start", "die", "destroy", "restart":
					select {
					case changes <- struct{}{}:
					default:
					}
				}
			}
			cmd.Wait()
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}
