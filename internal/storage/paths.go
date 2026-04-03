package storage

import "fmt"

func GetProjectStatePath(projectID string) string {
	return fmt.Sprintf("projects/%s/terraform.tfstate", projectID)
}
