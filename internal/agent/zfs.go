package agent

import "os/exec"

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

func GetMountpoint(dataset string) *exec.Cmd {
	return exec.Command("sudo", "zfs", "get", "-H", "-o", "value", "mountpoint", dataset)
}
