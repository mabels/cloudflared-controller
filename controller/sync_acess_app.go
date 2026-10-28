package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudflare/cloudflare-go/v7"
	"github.com/cloudflare/cloudflare-go/v7/option"
	"github.com/cloudflare/cloudflare-go/v7/zero_trust"
	"github.com/mabels/cloudflared-controller/controller/cfclient"
	"github.com/mabels/cloudflared-controller/controller/types"
	"github.com/rs/zerolog"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// https://developers.cloudflare.com/api/operations/access-applications-add-an-application

// ReadAccessGroupConfigMap reads access groups (Cloudflare API JSON) from the configured configmaps.
func ReadAccessGroupConfigMap(cfc types.CFController) ([]zero_trust.AccessGroupGetResponse, error) {
	out := []zero_trust.AccessGroupGetResponse{}
	for _, cm := range cfc.Cfg().AccessGroup.ConfigMapsNames {
		splitted := strings.Split(cm, "/")
		if len(splitted) > 2 {
			cfc.Log().Error().Str("name", cm).Msg("unknown name format")
			continue
		}
		ns := "default"
		name := splitted[0]
		if len(splitted) == 2 {
			ns = splitted[0]
			name = splitted[1]
		}
		cm, err := cfc.Rest().K8s().CoreV1().ConfigMaps(ns).Get(cfc.Context(), name, v1.GetOptions{})
		if err != nil {
			cfc.Log().Error().Str("ns", ns).Str("name", name).Err(err).Msg("can't get configmap")
			continue
		}
		for key, data := range cm.Data {
			ag := zero_trust.AccessGroupGetResponse{}
			err := json.Unmarshal([]byte(data), &ag)
			if err != nil || ag.Name == "" {
				cfc.Log().Error().Str("ns", ns).Str("name", name).Str("key", key).Err(err).Msg("is not an AccessGroup")
				continue
			}
			out = append(out, ag)
		}
	}
	return out, nil
}

func ReadCFAccessGroups(cfc types.CFController) ([]zero_trust.AccessGroupListResponse, error) {
	return GetAccessGroups(cfc.Context(), cfc.Rest().CF().API(), cfc.Rest().CF().AccountID())
}

func MergeAccessGroups(cmAg []zero_trust.AccessGroupGetResponse, cfAg []zero_trust.AccessGroupListResponse) (toAdd, toUpd []zero_trust.AccessGroupGetResponse) {
	cfMapAg := map[string]zero_trust.AccessGroupListResponse{}
	for _, ag := range cfAg {
		cfMapAg[ag.Name] = ag
	}
	for _, ag := range cmAg {
		cf, found := cfMapAg[ag.Name]
		if found {
			ag.ID = cf.ID
			toUpd = append(toUpd, ag)
		} else {
			toAdd = append(toAdd, ag)
		}
	}
	return toAdd, toUpd
}

// accessGroupBody sends the rules as they were written, the v7 rule params
// are a union per rule type and the configmap json is already in API format.
func accessGroupBody(ag zero_trust.AccessGroupGetResponse) ([]byte, error) {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal([]byte(ag.JSON.RawJSON()), &raw); err != nil {
		return nil, err
	}
	body := map[string]json.RawMessage{}
	for _, k := range []string{"name", "include", "exclude", "require", "is_default"} {
		if v, ok := raw[k]; ok {
			body[k] = v
		}
	}
	return json.Marshal(body)
}

func ApplyAccessGroups(cfc types.CFController, toAdd, toUpd []zero_trust.AccessGroupGetResponse) error {
	api := cfc.Rest().CF().API()
	accountID := cfc.Rest().CF().AccountID()
	for _, addAg := range toAdd {
		body, err := accessGroupBody(addAg)
		if err != nil {
			return err
		}
		_, err = api.ZeroTrust.Access.Groups.New(cfc.Context(), zero_trust.AccessGroupNewParams{
			AccountID: cloudflare.F(accountID),
		}, option.WithRequestBody("application/json", body))
		if err != nil {
			return err
		}
	}
	for _, updAg := range toUpd {
		body, err := accessGroupBody(updAg)
		if err != nil {
			return err
		}
		_, err = api.ZeroTrust.Access.Groups.Update(cfc.Context(), updAg.ID, zero_trust.AccessGroupUpdateParams{
			AccountID: cloudflare.F(accountID),
		}, option.WithRequestBody("application/json", body))
		if err != nil {
			return err
		}
	}
	return nil
}

func FindIdpByName(ctx context.Context, api *cloudflare.Client, accountID string, name string) (*zero_trust.IdentityProviderListResponse, error) {
	iter := api.ZeroTrust.IdentityProviders.ListAutoPaging(ctx, zero_trust.IdentityProviderListParams{
		AccountID: cloudflare.F(accountID),
	})
	for iter.Next() {
		idp := iter.Current()
		if idp.Name == name {
			return &idp, nil
		}
	}
	return nil, iter.Err()
}

func allowsIdp(app zero_trust.AccessApplicationListResponse, idpID string) bool {
	idps, ok := app.AllowedIdPs.([]interface{})
	if !ok {
		return false
	}
	for _, i := range idps {
		if s, ok := i.(string); ok && s == idpID {
			return true
		}
	}
	return false
}

func FindAccessAppsByIdpAndName(ctx context.Context, api *cloudflare.Client, accountID string, idp *zero_trust.IdentityProviderListResponse, appName string) ([]zero_trust.AccessApplicationListResponse, error) {
	ret := []zero_trust.AccessApplicationListResponse{}
	iter := api.ZeroTrust.Access.Applications.ListAutoPaging(ctx, zero_trust.AccessApplicationListParams{
		AccountID: cloudflare.F(accountID),
	})
	for iter.Next() {
		app := iter.Current()
		if app.Name == appName && allowsIdp(app, idp.ID) {
			ret = append(ret, app)
		}
	}
	return ret, iter.Err()
}

// UpsetAccessApp returns the id of the existing or created self hosted application.
func UpsetAccessApp(ctx context.Context, api *cloudflare.Client, accountID string, idp *zero_trust.IdentityProviderListResponse, name string, domain string) (string, error) {
	apps, err := FindAccessAppsByIdpAndName(ctx, api, accountID, idp, name)
	if err != nil {
		return "", err
	}
	if len(apps) == 1 {
		return apps[0].ID, nil
	}
	aa, err := api.ZeroTrust.Access.Applications.New(ctx, zero_trust.AccessApplicationNewParams{
		AccountID: cloudflare.F(accountID),
		Body: zero_trust.AccessApplicationNewParamsBodySelfHostedApplication{
			AllowedIdPs:       cloudflare.F([]zero_trust.AllowedIdPsParam{idp.ID}),
			Domain:            cloudflare.F(domain),
			Name:              cloudflare.F(name),
			SelfHostedDomains: cloudflare.F([]zero_trust.SelfHostedDomainsParam{domain}),
			Type:              cloudflare.F(zero_trust.ApplicationTypeSelfHosted),
		},
	})
	if err != nil {
		return "", err
	}
	return aa.ID, nil
}

func GetAccessPoliciesByApp(ctx context.Context, api *cloudflare.Client, accountID string, appID string) ([]zero_trust.AccessApplicationPolicyListResponse, error) {
	laps := []zero_trust.AccessApplicationPolicyListResponse{}
	iter := api.ZeroTrust.Access.Applications.Policies.ListAutoPaging(ctx, appID, zero_trust.AccessApplicationPolicyListParams{
		AccountID: cloudflare.F(accountID),
	})
	for iter.Next() {
		laps = append(laps, iter.Current())
	}
	return laps, iter.Err()
}

func GetAccessGroups(ctx context.Context, api *cloudflare.Client, accountID string) ([]zero_trust.AccessGroupListResponse, error) {
	lags := []zero_trust.AccessGroupListResponse{}
	iter := api.ZeroTrust.Access.Groups.ListAutoPaging(ctx, zero_trust.AccessGroupListParams{
		AccountID: cloudflare.F(accountID),
	})
	for iter.Next() {
		lags = append(lags, iter.Current())
	}
	return lags, iter.Err()
}

// CreateAccessApp is the --test-create-access experiment.
func CreateAccessApp(log *zerolog.Logger, cfg *types.CFControllerConfig) error {
	cf := cfclient.New(cfg.CloudFlare.ApiUrl, cfg.CloudFlare.ApiToken, cfg.CloudFlare.AccountId, log)
	api := cf.API()
	ctx := context.Background()

	idp, err := FindIdpByName(ctx, api, cf.AccountID(), "Google")
	if err != nil {
		return err
	}
	if idp == nil {
		return fmt.Errorf("idp not found")
	}

	appID, err := UpsetAccessApp(ctx, api, cf.AccountID(), idp, "cr-opensea-name", "cr-opensea.6265746f6f.tech")
	if err != nil {
		return err
	}
	fmt.Println("app:", appID)

	pbas, err := GetAccessPoliciesByApp(ctx, api, cf.AccountID(), appID)
	if err != nil {
		return err
	}
	for _, p := range pbas {
		fmt.Println(p.JSON.RawJSON())
	}

	ags, err := GetAccessGroups(ctx, api, cf.AccountID())
	if err != nil {
		return err
	}
	for _, p := range ags {
		fmt.Println(p.JSON.RawJSON())
	}
	return nil
}
