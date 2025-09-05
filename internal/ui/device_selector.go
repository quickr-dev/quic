package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/quickr-dev/quic/internal/ssh"
)

type DeviceSelector struct {
	devices      []ssh.BlockDevice
	cursor       int
	selected     map[int]bool
	done         bool
	cancelled    bool
	windowHeight int
	windowWidth  int
}

var (
	boldStyle        = lipgloss.NewStyle().Bold(true)
	unavailableStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	helpStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
)

func NewDeviceSelector(devices []ssh.BlockDevice) *DeviceSelector {
	return &DeviceSelector{
		devices:  devices,
		selected: make(map[int]bool),
	}
}

func (m *DeviceSelector) Init() tea.Cmd {
	return nil
}

func (m *DeviceSelector) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.windowHeight = msg.Height
		m.windowWidth = msg.Width

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			m.done = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.devices)-1 {
				m.cursor++
			}

		case " ":
			// Only allow selection of available devices
			if m.devices[m.cursor].Status == ssh.Available {
				m.selected[m.cursor] = !m.selected[m.cursor]
			}

		case "enter":
			if m.hasSelectedDevices() {
				m.done = true
				return m, tea.Quit
			}
		}
	}

	return m, nil
}

func (m *DeviceSelector) View() string {
	if m.windowWidth == 0 {
		return "Initializing..."
	}

	var b strings.Builder

	b.WriteString(boldStyle.Render("Select block devices for ZFS pool"))
	b.WriteString("\n\n")

	// Header
	header := fmt.Sprintf("%-6s %-20s %-10s %-10s %-15s", "", "NAME", "SIZE", "USED", "STATUS")
	b.WriteString(boldStyle.Render(header))
	b.WriteString("\n")

	// Devices
	for i, device := range m.devices {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}

		checkbox := "[ ]"
		if m.selected[i] {
			checkbox = "[x]"
		}

		name := device.Name
		size := formatSize(device.Size)
		used := ""
		if device.FSSize != nil {
			used = formatSize(*device.FSSize)
		}

		status := string(device.Status)
		if device.Reason != "" {
			status += fmt.Sprintf(" (%s)", device.Reason)
		}

		line := fmt.Sprintf("%s%-4s %-20s %-10s %-10s %-15s", cursor, checkbox, name, size, used, status)

		if device.Status != ssh.Available {
			line = unavailableStyle.Render(line)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Help text
	b.WriteString("\n")
	helpText := []string{
		"↑/↓ or k/j: navigate",
		"space: select/deselect",
		"enter: confirm selection",
		"q or esc: cancel",
	}
	b.WriteString(helpStyle.Render(strings.Join(helpText, " • ")))

	if !m.hasSelectedDevices() {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Select at least one available device to continue"))
	}

	return b.String()
}

func (m *DeviceSelector) hasSelectedDevices() bool {
	for i, selected := range m.selected {
		if selected && m.devices[i].Status == ssh.Available {
			return true
		}
	}
	return false
}

func (m *DeviceSelector) GetSelectedDevices() []string {
	var devices []string
	for i, selected := range m.selected {
		if selected && m.devices[i].Status == ssh.Available {
			devices = append(devices, m.devices[i].Name)
		}
	}
	return devices
}

func (m *DeviceSelector) IsDone() bool {
	return m.done
}

func (m *DeviceSelector) IsCancelled() bool {
	return m.cancelled
}

func RunDeviceSelector(devices []ssh.BlockDevice) ([]string, error) {
	model := NewDeviceSelector(devices)

	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("failed to run device selector: %w", err)
	}

	selector := finalModel.(*DeviceSelector)
	if selector.IsCancelled() {
		return nil, fmt.Errorf("device selection cancelled")
	}

	return selector.GetSelectedDevices(), nil
}

func formatSize(bytes int64) string {
	if bytes == 0 {
		return ""
	}

	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%dB", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"K", "M", "G", "T", "P", "E"}
	return fmt.Sprintf("%.1f%s", float64(bytes)/float64(div), units[exp])
}
