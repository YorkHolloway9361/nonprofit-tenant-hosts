package nonprofit

import (
	"context"
	"reflect"
	"testing"
)

type fakeProvisioner struct {
	calls []string
}

func (f *fakeProvisioner) AddDomain(_ context.Context, domain string, metadata map[string]string) (string, error) {
	f.calls = append(f.calls, "domain:"+domain+":"+metadata["tenant_id"])
	return "zone_42", nil
}

func (f *fakeProvisioner) UpsertCNAME(_ context.Context, zoneID, name, content, operationID string) error {
	f.calls = append(f.calls, "cname:"+zoneID+":"+name+":"+content+":"+operationID)
	return nil
}

func (f *fakeProvisioner) CreateBucket(_ context.Context, name string) error {
	f.calls = append(f.calls, "bucket:"+name)
	return nil
}

func (f *fakeProvisioner) PresignPut(_ context.Context, bucket, key, contentType, operationID string, maxBytes int64) (string, error) {
	f.calls = append(f.calls, "presign:"+bucket+":"+key+":"+contentType+":"+operationID)
	return "https://uploads.example/signed", nil
}

func TestProvisionPartitionsTenantDataAfterDNSZoneExists(t *testing.T) {
	tests := []struct {
		name       string
		input      TenantInput
		wantHost   string
		wantObject string
		wantCalls  []string
	}{
		{
			name:       "food bank campaign",
			input:      TenantInput{TenantID: "np-104", Slug: "harbor-food-bank", CampaignID: "winter-2026"},
			wantHost:   "harbor-food-bank.nonprofits.example.org",
			wantObject: "tenants/np-104/campaign-reports/winter-2026.json",
			wantCalls: []string{
				"domain:harbor-food-bank.nonprofits.example.org:np-104",
				"cname:zone_42:harbor-food-bank.nonprofits.example.org:assets.example.net:tenant-np-104-cname",
				"bucket:nonprofit-assets",
				"presign:nonprofit-assets:tenants/np-104/campaign-reports/winter-2026.json:application/json:tenant-np-104-winter-2026",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeProvisioner{}
			pipeline := Pipeline{Infrai: fake, ParentDomain: "nonprofits.example.org", AssetOrigin: "assets.example.net", Bucket: "nonprofit-assets"}
			got, err := pipeline.Provision(context.Background(), tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got.Hostname != tt.wantHost || got.CampaignObjectKey != tt.wantObject {
				t.Fatalf("got host %q and object %q", got.Hostname, got.CampaignObjectKey)
			}
			if got.DatasetPrefixes["donor_receipts"] != "tenants/np-104/donor-receipts/" {
				t.Fatalf("unexpected receipt partition: %q", got.DatasetPrefixes["donor_receipts"])
			}
			if !reflect.DeepEqual(fake.calls, tt.wantCalls) {
				t.Fatalf("call order\n got: %#v\nwant: %#v", fake.calls, tt.wantCalls)
			}
		})
	}
}
