package nonprofit

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

type Provisioner interface {
	AddDomain(context.Context, string, map[string]string) (string, error)
	UpsertCNAME(context.Context, string, string, string, string) error
	CreateBucket(context.Context, string) error
	PresignPut(context.Context, string, string, string, string, int64) (string, error)
}

type Pipeline struct {
	Infrai       Provisioner
	ParentDomain string
	AssetOrigin  string
	Bucket       string
}

type TenantInput struct {
	TenantID   string `json:"tenant_id"`
	Slug       string `json:"slug"`
	CampaignID string `json:"campaign_id"`
}

type TenantOutput struct {
	TenantID          string            `json:"tenant_id"`
	Hostname          string            `json:"hostname"`
	ZoneID            string            `json:"zone_id"`
	UploadURL         string            `json:"upload_url"`
	CampaignObjectKey string            `json:"campaign_object_key"`
	DatasetPrefixes   map[string]string `json:"dataset_prefixes"`
}

func (p Pipeline) Provision(ctx context.Context, in TenantInput) (TenantOutput, error) {
	if strings.TrimSpace(in.TenantID) == "" || strings.TrimSpace(in.CampaignID) == "" || !slugPattern.MatchString(in.Slug) {
		return TenantOutput{}, fmt.Errorf("tenant_id, campaign_id, and a DNS-safe slug are required")
	}

	hostname := in.Slug + "." + p.ParentDomain
	operationID := "tenant-" + in.TenantID
	zoneID, err := p.Infrai.AddDomain(ctx, hostname, map[string]string{"tenant_id": in.TenantID})
	if err != nil {
		return TenantOutput{}, fmt.Errorf("add tenant domain: %w", err)
	}
	if err := p.Infrai.UpsertCNAME(ctx, zoneID, hostname, p.AssetOrigin, operationID+"-cname"); err != nil {
		return TenantOutput{}, fmt.Errorf("route tenant hostname: %w", err)
	}
	if err := p.Infrai.CreateBucket(ctx, p.Bucket); err != nil {
		return TenantOutput{}, fmt.Errorf("prepare asset bucket: %w", err)
	}

	prefix := "tenants/" + in.TenantID + "/"
	objectKey := prefix + "campaign-reports/" + in.CampaignID + ".json"
	uploadURL, err := p.Infrai.PresignPut(ctx, p.Bucket, objectKey, "application/json", operationID+"-"+in.CampaignID, 5<<20)
	if err != nil {
		return TenantOutput{}, fmt.Errorf("sign campaign report upload: %w", err)
	}

	return TenantOutput{
		TenantID: in.TenantID, Hostname: hostname, ZoneID: zoneID,
		UploadURL: uploadURL, CampaignObjectKey: objectKey,
		DatasetPrefixes: map[string]string{
			"donor_receipts":      prefix + "donor-receipts/",
			"volunteer_reminders": prefix + "volunteer-reminders/",
			"campaign_reports":    prefix + "campaign-reports/",
		},
	}, nil
}
