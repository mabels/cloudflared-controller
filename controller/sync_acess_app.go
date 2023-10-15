package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	// "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	// "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	cfgo "github.com/cloudflare/cloudflare-go"
	"github.com/mabels/cloudflared-controller/controller/types"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// https://developers.cloudflare.com/api/operations/zone-level-access-applications-add-a-bookmark-application?schema_url=https%3A%2F%2Fraw.githubusercontent.com%2Fcloudflare%2Fapi-schemas%2Fmain%2Fopenapi.yaml#Request
// https://developers.cloudflare.com/api/operations/access-applications-add-an-application
// /Users/menabe/Software/cloudflare-go/access_application.go
// Request URL:
// https://dash.cloudflare.com/api/v4/accounts/5cb0543d283f09fdcd2e2b11dcb00b6b/access/apps
// Request Method:
// POST
// Status Code:
// 201 Created
// Remote Address:
// 104.17.110.184:443
// Referrer Policy:
// strict-origin-when-cross-origin

// {
//   "name": "meno-adviser-ui",
//   "logo_url": "",
//   "allowed_idps": [
//     "3ed2350c-7283-453b-be68-7f160bded525"
//   ],
//   "auto_redirect_to_identity": false,
//   "service_auth_401_redirect": false,
//   "policies": [
//     {
//       "decision": "allow",
//       "name": "default",
//       "include": [
//         {
//           "email_domain": {
//             "domain": "adviser.ai"
//           }
//         },
//         {
//           "email_domain": {
//             "domain": "adviser.io"
//           }
//         }
//       ],
//       "exclude": [],
//       "require": [],
//       "precedence": 1
//     }
//   ],
//   "session_duration": "24h",
//   "app_launcher_visible": true,
//   "self_hosted_domains": [
//     "meno-adviser-ui.6265746f6f.tech"
//   ],
//   "type": "self_hosted",
//   "selected_app_groups": [],
//   "custom_deny_url": "",
//   "custom_pages": [],
//   "custom_deny_message": "",
//   "domain": "meno-adviser-ui.6265746f6f.tech",
//   "tags": []
// }

// func fullList[R any](fn func() (R, *cfgo.ResultInfo, error)) (rs []R, err error) {
// 	var rc *cfgo.ResultInfo
// 	var r R
// 	for next := true; next; {
// 		var rs []R
// 		r, rc, err = fn()
// 		if err != nil {
// 			return rs, err
// 		}
// 		rt := reflect.TypeOf(r)
// 		switch rt.Kind() {
// 		case reflect.Slice, reflect.Array:
// 			rs = append(rs, (r.([]R))...)
// 		default:
// 			rs = append(rs, r)
// 		}
// 		next = rc.Done()
// 	}
// }

func ReadAccessGroupConfigMap(cfc types.CFController) ([]cfgo.AccessGroup, error) {
	out := []cfgo.AccessGroup{}
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
			ag := cfgo.AccessGroup{}
			err := json.Unmarshal([]byte(data), &ag)
			if err != nil {
				cfc.Log().Error().Str("ns", ns).Str("name", name).Str("key", key).Str("data", data).Err(err).Msg("is not an AccessGroup")
				continue
			}
			out = append(out, ag)
		}
	}
	return out, nil
}

func ReadCFAccessGroups(cfc types.CFController) ([]cfgo.AccessGroup, error) {
	cfgoApi, err := cfc.Rest().Cfgo()
	if err != nil {
		return nil, err
	}
	out := []cfgo.AccessGroup{}
	var rc *cfgo.ResultInfo
	for next := true; next; {
		var lag []cfgo.AccessGroup
		lag, rc, err = cfgoApi.ListAccessGroups(cfc.Context(), &cfgo.ResourceContainer{
			Level:      "accounts",
			Identifier: cfc.Cfg().CloudFlare.AccountId,
		}, cfgo.ListAccessGroupsParams{})
		if err != nil {
			cfc.Log().Error().Err(err).Msg("ListAccessGroups")
			break
		}
		out = append(out, lag...)
		next = rc.Done()
	}
	return out, nil

}

func MergeAccessGroups(cmAg, cfAg []cfgo.AccessGroup) (toAdd, toUpd []cfgo.AccessGroup) {
	cfMapAg := map[string]cfgo.AccessGroup{}
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

func ApplyAccessGroups(cfc types.CFController, toAdd, toUpd []cfgo.AccessGroup) error {
	api, err := cfc.Rest().Cfgo()
	if err != nil {
		return err
	}
	for _, addAg := range toAdd {
		rc := cfgo.ResourceContainer{}
		_, err := api.CreateAccessGroup(cfc.Context(), &rc, cfgo.CreateAccessGroupParams{
			Name:    addAg.Name,
			Include: addAg.Include,
			Exclude: addAg.Exclude,
			Require: addAg.Require,
		})
		if err != nil {
			return err
		}
	}
	for _, updAg := range toUpd {
		rc := cfgo.ResourceContainer{}
		_, err := api.UpdateAccessGroup(cfc.Context(), &rc, cfgo.UpdateAccessGroupParams{
			ID:      updAg.ID,
			Name:    updAg.Name,
			Include: updAg.Include,
			Exclude: updAg.Exclude,
			Require: updAg.Require,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// readConfigMap AccessGroups
// readCFAccessGroups

// find ConfigMap AccessGroup in CF

func FindIdpByName(ctx context.Context, cfg *types.CFControllerConfig, api *cfgo.API, name string) (*cfgo.AccessIdentityProvider, error) {
	var rc cfgo.ListAccessIdentityProvidersParams
	for next := true; next; {
		idps, rc, err := api.ListAccessIdentityProviders(ctx, &cfgo.ResourceContainer{
			Level:      "accounts",
			Identifier: cfg.CloudFlare.AccountId,
		}, rc)
		if err != nil {
			return nil, err
		}
		for _, a := range idps {
			if a.Name == name {
				return &a, nil
			}
		}
		next = rc.Done()
	}
	return nil, nil
}

func FindAccessAppsByIdpAndName(ctx context.Context, cfg *types.CFControllerConfig, api *cfgo.API, idp *cfgo.AccessIdentityProvider, appName string) ([]cfgo.AccessApplication, error) {
	var rc cfgo.ListAccessApplicationsParams
	ret := []cfgo.AccessApplication{}
	for next := true; next; {
		laa, rc, err := api.ListAccessApplications(ctx, &cfgo.ResourceContainer{
			Level:      "accounts",
			Identifier: cfg.CloudFlare.AccountId,
		}, rc)
		if err != nil {
			return nil, err
		}
		for _, a := range laa {
			for _, i := range a.AllowedIdps {
				if i == idp.ID && a.Name == appName {
					ret = append(ret, a)
				}
			}
		}
		next = rc.Done()
	}
	return ret, nil
}

func UpsetAccessApp(ctx context.Context, cfg *types.CFControllerConfig, api *cfgo.API, idp *cfgo.AccessIdentityProvider, name string) (*cfgo.AccessApplication, error) {

	apps, err := FindAccessAppsByIdpAndName(ctx, cfg, api, idp, name)
	if err != nil {
		return nil, err
	}
	if len(apps) == 1 {
		return &apps[0], nil
	}

	// for _, a := range apps {
	// 	bytes, _ := json.Marshal(a)
	// 	fmt.Println(string(bytes))
	// }

	/*
		{
			"allowed_idps": [
			  "3ed2350c-7283-453b-be68-7f160bded525"
			],
			"aud": "eda6cb0521c2b5bc0a886715ce02c435fb0c6a5f84984264ff8d030177c5f2bf",
			"domain": "rabbitmq.6265746f6f.tech",
			"self_hosted_domains": [
			  "rabbitmq.6265746f6f.tech"
			],
			"type": "self_hosted",
			"session_duration": "24h",
			"name": "rabbit-mq",
			"id": "90e3ffab-6200-4bca-8ecd-629f5db0ad79",
			"private_address": "",
			"created_at": "2023-09-20T12:37:09Z",
			"updated_at": "2023-09-22T06:37:45Z",
			"auto_redirect_to_identity": false,
			"app_launcher_visible": true,
			"enable_binding_cookie": false,
			"http_only_cookie_attribute": true
		}
	*/
	aa, err := api.CreateAccessApplication(ctx, &cfgo.ResourceContainer{
		Level:      "accounts",
		Identifier: cfg.CloudFlare.AccountId,
	}, cfgo.CreateAccessApplicationParams{
		AllowedIdps: []string{idp.ID},
		// AppLauncherVisible:       true,
		// AUD:                      "",
		// AutoRedirectToIdentity:   new(bool),
		// CorsHeaders:              &cfgo.AccessApplicationCorsHeaders{},
		// CustomDenyMessage:        "",
		// CustomDenyURL:            "",
		// CustomNonIdentityDenyURL: "",
		Domain: "cr-opensea.6265746f6f.tech",
		// EnableBindingCookie:      new(bool),
		// GatewayRules:             []cfgo.AccessApplicationGatewayRule{},
		// HttpOnlyCookieAttribute: new(bool),
		// LogoURL:                 "",
		Name: "cr-opensea-name",
		// PathCookieAttribute:     new(bool),
		// PrivateAddress:          "",
		// SaasApplication:         &cfgo.SaasApplication{},
		// SameSiteCookieAttribute: "",
		SelfHostedDomains: []string{
			"cr-opensea.6265746f6f.tech",
		},
		// ServiceAuth401Redirect:  new(bool),
		// SessionDuration:         "",
		// SkipInterstitial:        new(bool),
		Type: "self_hosted",
		// CustomPages:             []string{},
	})
	if err != nil {
		log.Fatal(err)
	}
	bytes, _ := json.Marshal(aa)
	fmt.Println(string(bytes))
	return &aa, nil
}

func GetAccessPoliciesByApp(ctx context.Context, cfg *types.CFControllerConfig, api *cfgo.API, app *cfgo.AccessApplication) (laps []cfgo.AccessPolicy, err error) {
	var rc *cfgo.ResultInfo
	for next := true; next; {
		var lap []cfgo.AccessPolicy
		lap, rc, err = api.ListAccessPolicies(ctx, &cfgo.ResourceContainer{
			Level:      "accounts",
			Identifier: cfg.CloudFlare.AccountId,
		}, cfgo.ListAccessPoliciesParams{
			ApplicationID: app.ID,
		})
		if err != nil {
			return laps, err
		}
		laps = append(laps, lap...)
		next = rc.Done()
	}
	return laps, err
}

func GetAccessGroups(ctx context.Context, cfg *types.CFControllerConfig, api *cfgo.API) (lags []cfgo.AccessGroup, err error) {
	var rc *cfgo.ResultInfo
	for next := true; next; {
		var lag []cfgo.AccessGroup
		lag, rc, err = api.ListAccessGroups(ctx, &cfgo.ResourceContainer{
			Level:      "accounts",
			Identifier: cfg.CloudFlare.AccountId,
		}, cfgo.ListAccessGroupsParams{})
		if err != nil {
			return lags, err
		}
		lags = append(lags, lag...)
		next = rc.Done()
	}
	return lags, err

}

func CreateAccessApp(cfg *types.CFControllerConfig) {
	fmt.Println("CreateAccessApp")
	// api, err := cfgo.New(os.Getenv("CLOUDFLARE_API_KEY"), os.Getenv("CLOUDFLARE_API_EMAIL"))
	// alternatively, you can use a scoped API token
	api, err := cfgo.NewWithAPIToken(cfg.CloudFlare.ApiToken)
	if err != nil {
		log.Fatal(err)
	}

	// Most API calls require a Context
	ctx := context.Background()

	idp, err := FindIdpByName(ctx, cfg, api, "Google")
	if err != nil {
		log.Fatal(err)
	}
	if idp == nil {
		log.Fatal("idp not found")
	}

	// "cr-opensea-name"
	app, err := UpsetAccessApp(ctx, cfg, api, idp, "cr-opensea-name")
	if err != nil {
		log.Fatal(err)
	}
	bytes, _ := json.Marshal(app)
	fmt.Println(string(bytes))

	pbas, err := GetAccessPoliciesByApp(ctx, cfg, api, app)
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range pbas {
		bytes, _ := json.Marshal(p)
		fmt.Println(string(bytes))
	}

	/*
		{
		  "result": {
		    "id": "64b35a1c-dfd2-4866-88fb-8b49f8fe0cd2",
		    "uid": "64b35a1c-dfd2-4866-88fb-8b49f8fe0cd2",
		    "type": "self_hosted",
		    "name": "cr-opensea-name",
		    "aud": "032c4c0f71f50fbd98cb05863be4ca13770fe9a12dac753cb0c826088e30bac8",
		    "created_at": "0001-01-01T00:00:00Z",
		    "updated_at": "2023-09-24T18:25:08Z",
		    "domain": "cr-opensea.6265746f6f.tech",
		    "self_hosted_domains": [
		      "cr-opensea.6265746f6f.tech"
		    ],
		    "app_launcher_visible": true,
		    "allowed_idps": [
		      "3ed2350c-7283-453b-be68-7f160bded525"
		    ],
		    "tags": [],
		    "auto_redirect_to_identity": false,
		    "policies": [
		      {
		        "created_at": "2023-09-24T18:25:08Z",
		        "decision": "allow",
		        "exclude": [],
		        "id": "f72f4441-4d5c-4974-9a87-4242d7d61ec6",
		        "include": [
		          {
		            "group": {
		              "id": "20c69cb4-0553-467a-9dca-c8419acad708"
		            }
		          }
		        ],
		        "name": "blub",
		        "require": [],
		        "uid": "f72f4441-4d5c-4974-9a87-4242d7d61ec6",
		        "updated_at": "2023-09-24T18:25:08Z",
		        "precedence": 2
		      }
		    ],
		    "session_duration": "24h",
		    "enable_binding_cookie": false,
		    "http_only_cookie_attribute": true
		  },
		  "success": true,
		  "errors": [],
		  "messages": []
		}

				Policy
				{
				  "id": "cffb58f6-a802-4c82-98b5-0d7f44b11d3e",
				  "precedence": 2,
				  "decision": "allow",
				  "created_at": "2023-09-24T18:20:17Z",
				  "updated_at": "2023-09-24T18:20:17Z",
				  "name": "blabla",
				  "approval_groups": null,
				  "include": [
				    {
				      "group": {
				        "id": "20c69cb4-0553-467a-9dca-c8419acad708"
				      }
				    }
				  ],
				  "exclude": [],
				  "require": []
				}
				Group
				{
					"id": "20c69cb4-0553-467a-9dca-c8419acad708",
					"created_at": "2023-09-22T07:13:15Z",
					"updated_at": "2023-09-22T07:13:15Z",
					"name": "mam-hh-nuc",
					"include": [
						{
						"email_domain": {
							"domain": "adviser.ai"
						}
						},
						{
						"email_domain": {
							"domain": "adviser.io"
						}
						}
					],
					"exclude": [],
					"require": []
					}
	*/
	ags, err := GetAccessGroups(ctx, cfg, api)
	if err != nil {
		log.Fatal(err)
	}
	for _, p := range ags {
		bytes, _ := json.Marshal(p)
		fmt.Println(string(bytes))
	}

}
