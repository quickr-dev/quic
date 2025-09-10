package cli

import (
	"fmt"

	"github.com/quickr-dev/quic/internal/config"
)

func getTemplateName(cfg *config.UserConfig, flagValue string) (string, error) {
	templateName := flagValue
	if templateName == "" {
		templateName = cfg.DefaultTemplate
	}
	if templateName == "" {
		return "", fmt.Errorf("template not specified. Use --template flag or set defaultTemplate in config")
	}
	return templateName, nil
}

func getRestoreName(cfg *config.UserConfig, flagValue string) (string, error) {
	templateName := flagValue
	if templateName == "" {
		templateName = cfg.DefaultTemplate
	}
	if templateName == "" {
		return "", fmt.Errorf("restore template not specified. Use --restore flag or set defaultTemplate in config")
	}
	return templateName, nil
}
