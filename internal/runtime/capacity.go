package runtime

import (
	"errors"
	"golang.org/x/sys/unix"
	"os"
	"strconv"
	"strings"
)

func HostCapacity(directory string, candidateMiB int) error {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	var fs unix.Statfs_t
	if err := unix.Statfs(directory, &fs); err != nil {
		return err
	}
	if uint64(fs.Bavail)*uint64(fs.Bsize) < 2*1024*1024*1024 {
		return errors.New("less than 2 GiB free disk; retain live version")
	}
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[0] == "MemAvailable:" {
				n, _ := strconv.ParseInt(f[1], 10, 64)
				if n < int64(candidateMiB+128)*1024 {
					return errors.New("host memory headroom insufficient; retain live version")
				}
			}
		}
	}
	return nil
}
