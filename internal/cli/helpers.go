package cli

import (
	"fmt"

	"github.com/quickr-dev/quic/internal/config"
)

func GetTemplate(templateFlag string) (*config.Template, error) {
	userCfg, err := config.LoadUserConfig()
	if err != nil {
		return nil, fmt.Errorf("loading user config: %w", err)
	}

	projectCfg, err := config.LoadProjectConfig()
	if err != nil {
		return nil, fmt.Errorf("loading project config: %w", err)
	}

	// Use flag or user config default
	templateName := templateFlag
	if templateName == "" {
		templateName = userCfg.DefaultTemplate
	}

	// If no template specified
	if templateName == "" {
		// and project config has exactly one template, use it
		if len(projectCfg.Templates) == 1 {
			return &projectCfg.Templates[0], nil
		}
		// otherwise, return a nice error
		if len(projectCfg.Templates) == 0 {
			return nil, fmt.Errorf("no templates configured in project config")
		}
		return nil, fmt.Errorf("multiple templates available. Use the --template flag to specify one")
	}

	// Validate template exists in project config
	for _, template := range projectCfg.Templates {
		if template.Name == templateName {
			return &template, nil
		}
	}

	return nil, fmt.Errorf("template '%s' not found in project config", templateName)
}
