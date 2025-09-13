package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	ZPool = "tank"
)

func GetTemplateDataset(template string) string {
	return ZPool + "/" + template
}

func GetBranchDataset(template, branch string) string {
	return ZPool + "/" + template + "/" + branch
}

func GetSnapshotName(template, branch string) string {
	return ZPool + "/" + template + "@" + branch
}

func GetBranchMountpoint(template, branch string) string {
	return "/opt/quic/" + template + "/" + branch
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

func destroyDataset(dataset string, flags ...string) error {
	args := []string{"zfs", "destroy"}
	args = append(args, flags...)
	args = append(args, dataset)

	output, err := exec.Command("sudo", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("destroying ZFS dataset %s: %s", dataset, output)
	}

	return nil
}

func createSnapshot(snapshotName string) error {
	cmd := exec.Command("sudo", "zfs", "snapshot", snapshotName)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating ZFS snapshot %s: %w", snapshotName, err)
	}

	return nil
}

func createClone(snapshot string, dataset string, mountpoint string) error {
	cmd := exec.Command("sudo", "zfs", "clone", "-o", "mountpoint="+mountpoint, snapshot, dataset)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating ZFS clone: %w", err)
	}

	return nil
}

func listDatasets(filterByDataset string) ([]string, error) {
	cmd := exec.Command("sudo", "zfs", "list", "-H", "-o", "name", "-r", filterByDataset)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("listing ZFS datasets under %s: %s", filterByDataset, output)
	}

	var datasets []string
	lines := strings.SplitSeq(strings.TrimSpace(string(output)), "\n")

	for line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == filterByDataset {
			continue
		}
		datasets = append(datasets, line)
	}

	return datasets, nil
}
