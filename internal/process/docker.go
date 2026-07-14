package process

import (
	"fmt"
	"os/exec"
	"strings"
)

func FindDockerHolders(port int) ([]string, error) {
	cmd := exec.Command("docker", "ps",
		"--filter", fmt.Sprintf("publish=%d", port),
		"--format", "{{.Names}}",
	)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	names := strings.Fields(strings.TrimSpace(string(out)))
	return names, nil
}

func StopContainers(names []string) error {
	if len(names) == 0 {
		return nil
	}
	args := append([]string{"stop"}, names...)
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop %s: %w\n%s", strings.Join(names, " "), err, string(out))
	}
	return nil
}
