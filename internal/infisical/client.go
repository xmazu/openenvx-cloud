package infisical

import (
	"context"
	"fmt"

	infisical "github.com/infisical/go-sdk"
)

type SecretManager interface {
	GetSecrets(ctx context.Context, projectID, environment, path string) (map[string]string, error)
}

type Client struct {
	client infisical.InfisicalClientInterface
}

type Config struct {
	ClientID     string
	ClientSecret string
	SiteURL      string
}

func NewClient(cfg Config) (*Client, error) {
	client := infisical.NewInfisicalClient(context.Background(), infisical.Config{
		SiteUrl: cfg.SiteURL,
	})

	_, err := client.Auth().UniversalAuthLogin(cfg.ClientID, cfg.ClientSecret)
	if err != nil {
		return nil, fmt.Errorf("infisical auth failed: %w", err)
	}

	return &Client{
		client: client,
	}, nil
}

func (c *Client) GetSecrets(ctx context.Context, projectID, environment, path string) (map[string]string, error) {
	res, err := c.client.Secrets().ListSecrets(infisical.ListSecretsOptions{
		ProjectID:   projectID,
		Environment: environment,
		SecretPath:  path,
	})
	if err != nil {
		return nil, fmt.Errorf("list secrets for %s in %s at %s: %w", projectID, environment, path, err)
	}

	result := make(map[string]string)
	for _, secret := range res.Secrets {
		result[secret.SecretKey] = secret.SecretValue
	}

	return result, nil
}
