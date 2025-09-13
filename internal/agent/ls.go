package agent

import (
	"context"
	"fmt"
)

func (s *AgentService) ListBranches(ctx context.Context, template string) ([]*BranchInfo, error) {
	var filterByDataset string
	if template != "" {
		filterByDataset = GetTemplateDataset(template)
	} else {
		filterByDataset = ZPool
	}

	var branches []*BranchInfo

	datasets, err := listDatasets(filterByDataset)
	if err != nil {
		return branches, nil
	}

	for _, dataset := range datasets {
		branch, err := s.getBranchMetadata(dataset)
		if err != nil {
			fmt.Printf("Warning: failed to load branch %s: %v\n", dataset, err)
			continue
		}
		if branch != nil {
			branches = append(branches, branch)
		}
	}

	return branches, nil
}
