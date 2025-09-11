package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	ZPool = "tank"
)

func templateDataset(template string) string {
	return ZPool + "/" + template
}

func branchDataset(template, branch string) string {
	return ZPool + "/" + template + "/" + branch
}

func datasetExists(dataset string) bool {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", dataset)
	return cmd.Run() == nil
}

func snapshotExists(snapshot string) bool {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-t", "snapshot", snapshot)
	return cmd.Run() == nil
}

func GetMountpoint(dataset string) (string, error) {
	cmd := exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", dataset)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("getting ZFS mountpoint: %w", err)
	}

	mountpoint := strings.TrimSpace(string(output))
	if mountpoint == "none" || mountpoint == "-" || mountpoint == "" {
		return "", fmt.Errorf("invalid ZFS mountpoint'%s'", mountpoint)
	}

	return mountpoint, nil
}

func GetTemplateMountpoint(template string) (string, error) {
	return GetMountpoint(templateDataset(template))
}

func GetBranchMountpoint(template string, branch string) (string, error) {
	return GetMountpoint(branchDataset(template, branch))
}
