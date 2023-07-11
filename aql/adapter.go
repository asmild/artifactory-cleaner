package aql

import (
	"encoding/json"
	"fmt"
)

func (c *AQL) queryExec(query string) ([]byte, error) {
	const queryFormatOutput = ".include(\"path\", \"name\", \"size\",\"created\", \"updated\", \"stat.downloaded\")"
	//util.Debug(false,"fetchArtifacts query",fmt.Sprintf("%s%s",query,queryFormatOutput))

	bytes, err := c.client.Exec("/artifactory/api/search/aql", fmt.Sprintf("%s%s", query, queryFormatOutput))
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %s", err)
	}
	return bytes, err
}

func (c *AQL) deleteArtifact(repository string, artifactPath string) error {
	query := fmt.Sprintf("/artifactory/%s/%s", repository, artifactPath)
	_, err := c.client.ExecDelete(query)
	return err
}

func (c *AQL) fetchArtifacts(target string, pathMatcher string, discriminator string) ([]ArtifactView, error) {
	query := fmt.Sprintf("items.find({\"repo\":\"%s\",\"path\":{\"$match\": \"%s\"},\"name\":{\"$match\": \"%s\"}})", target, pathMatcher, discriminator)

	bytes, err := c.queryExec(query)
	if err != nil {
		return nil, err
	}

	var aqlResponse Results

	err = json.Unmarshal(bytes, &aqlResponse)
	if err != nil {
		return []ArtifactView{}, fmt.Errorf("json umarshall error: %s", err)
	}

	return aqlResponse.Artifacts, err
}

func (c *AQL) getArtifactChildren(target string, artifactPath string) ([]ArtifactView, error) {
	query := fmt.Sprintf("items.find({\"repo\":\"%s\",\"path\":{\"$match\": \"%s\"}})", target, artifactPath)

	// What path will match
	// Debug
	//util.Debug(true,"query:",query)
	// ----
	bytes, err := c.queryExec(query)
	if err != nil {
		return nil, err
	}

	var aqlResponse Results

	err = json.Unmarshal(bytes, &aqlResponse)
	if err != nil {
		return []ArtifactView{}, fmt.Errorf("json umarshall error: %s", err)
	}
	return aqlResponse.Artifacts, err
}
