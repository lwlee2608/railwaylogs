package api

import (
	"context"
	"fmt"
)

const latestDeploymentQuery = `query LatestDeployment($serviceId: String!, $environmentId: String!) {
  serviceInstance(environmentId: $environmentId, serviceId: $serviceId) {
    latestDeployment { id status }
  }
}`

type Deployment struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

// LatestDeployment returns the latest deployment for a given service+environment.
func (c *Client) LatestDeployment(ctx context.Context, serviceID, environmentID string) (*Deployment, error) {
	var resp struct {
		ServiceInstance struct {
			LatestDeployment *Deployment `json:"latestDeployment"`
		} `json:"serviceInstance"`
	}

	vars := map[string]string{
		"serviceId":     serviceID,
		"environmentId": environmentID,
	}
	if err := c.Query(ctx, latestDeploymentQuery, vars, &resp); err != nil {
		return nil, err
	}
	if resp.ServiceInstance.LatestDeployment == nil {
		return nil, fmt.Errorf("no deployment found for service %s in environment %s", serviceID, environmentID)
	}
	return resp.ServiceInstance.LatestDeployment, nil
}
