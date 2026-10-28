# cloudflared-controller

A Kubernetes controller that publishes Services and Ingresses through
[Cloudflare Tunnels](https://developers.cloudflare.com/cloudflare-one/connections/connect-networks/).
Annotate a Service or Ingress, and the controller creates the tunnel, the DNS
record and runs `cloudflared` for you. No CRDs, no public IP, no open ports.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: syncthing
  annotations:
    cloudflare.com/tunnel-name: home.example.com          # which tunnel
    cloudflare.com/tunnel-external-name: sync.example.com # public hostname
spec:
  ports:
    - name: http
      port: 8384
```

`https://sync.example.com` now reaches `http://syncthing.<namespace>:8384`.

## How it works

```
Service / Ingress ──annotations──▶ tunnel ConfigMap ──▶ Cloudflare tunnel + DNS
   (any namespace)                 cfd-tunnel-cfg.<tunnel>      (leader only)
                                          │
                                          └──▶ cloudflared process (every replica)
```

1. The controller watches Services and Ingresses with a
   `cloudflare.com/tunnel-name` annotation and writes their routes into one
   ConfigMap per tunnel (`cfd-tunnel-cfg.<tunnel>`), one key per resource.
2. For each tunnel ConfigMap the leader makes sure that
   - the Cloudflare tunnel `<cluster-name>/<namespace>/<tunnel>` exists,
   - its credentials are stored in the Secret `cfd-tunnel-key.<tunnel>`,
   - every hostname has a proxied CNAME to `<tunnel-id>.cfargotunnel.com`.
3. Every replica runs a `cloudflared` per tunnel ConfigMap with a locally
   generated config. Running more replicas gives more tunnel connectors.
   A crashed `cloudflared` is restarted after `--restart-delay`; a changed
   config starts a new `cloudflared` before the old one is stopped.

Removing the annotation (or the resource) removes its route. **Deleting a
tunnel ConfigMap deletes the Cloudflare tunnel and its Secret.**

DNS records are only created or updated, never deleted. An existing CNAME is
pointed to the tunnel; an existing A/AAAA record is left alone and logged as
error.

## Requirements

A Cloudflare API token and your Cloudflare account ID. The token needs two
permissions; a DNS-only token (like the one external-dns uses) is not enough,
the controller also manages the tunnels.

| Type    | Resource          | Permission |
|---------|-------------------|------------|
| Account | Cloudflare Tunnel | Edit       |
| Zone    | DNS               | Edit       |

*DNS: Edit* also allows listing the zones, so no extra *Zone* permission is needed.

`--test-create-access` additionally needs *Account → Access: Apps and Policies → Edit*.

### Creating the token

1. Open the Cloudflare dashboard → *My Profile* → *API Tokens*
   (<https://dash.cloudflare.com/profile/api-tokens>) → **Create Token**.
2. At *Custom token* choose **Get started**.
3. *Token name*: e.g. `cloudflared-controller <cluster>`.
4. *Permissions*, add the two rows from the table above:
   - `Account` · `Cloudflare Tunnel` · `Edit`
   - `Zone` · `DNS` · `Edit` (not `Read`, the controller creates the records)
5. *Account Resources*: `Include` · your account (or `All accounts`).
6. *Zone Resources*: `Include` · `Specific zone` · each domain you want to
   publish, or `All zones`.
7. Optional: *Client IP address filtering* with the public IP(s) of your
   cluster, and a *Token expiration*.
8. **Continue to summary** → **Create Token** and copy the token; it is shown
   only once.

An existing token can be changed the same way (*API Tokens* → *Edit*); its
value stays the same, so nothing has to change in the cluster.

Check it (prints `"status": "active"`):

```sh
curl -s https://api.cloudflare.com/client/v4/user/tokens/verify \
  -H "Authorization: Bearer <token>"
```

The account ID is on the *Overview* page of any of your domains (right column,
*API* → *Account ID*), or in the dashboard URL `dash.cloudflare.com/<account id>/`.

Store both in the cluster:

```sh
kubectl -n default create secret generic cloudflared-controller \
  --from-literal=CLOUDFLARE_ACCOUNT_ID=<account id> \
  --from-literal=CLOUDFLARE_API_TOKEN=<token>
```

To replace the token later, update the secret and restart the controller:

```sh
kubectl -n default create secret generic cloudflared-controller \
  --from-literal=CLOUDFLARE_ACCOUNT_ID=<account id> \
  --from-literal=CLOUDFLARE_API_TOKEN=<new token> \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl -n default rollout restart deploy/cloudflared-controller
```

The controller only reads tunnels, it never takes over a running tunnel's
credentials: if a tunnel is known (ConfigMap annotation `tunnel-id`) but the
token can't read it, the controller logs an error instead of creating a new
tunnel.

## Installation

### Helm

```sh
helm install cloudflared-controller ./helm/cloudflared-controller -n default \
  --set cloudflare.accountId=<account id> \
  --set cloudflare.apiToken=<api token>
```

To manage the secret yourself (see [Creating the token](#creating-the-token)),
pass `--set cloudflare.existingSecret=<name>`.

| Value                                | Default                                  | Description |
|--------------------------------------|------------------------------------------|-------------|
| `image.repository`                   | `ghcr.io/mabels/cloudflared-controller`  | |
| `image.tag`                          | chart `appVersion`                       | |
| `replicaCount`                       | `1`                                      | every replica runs the tunnels, one is leader |
| `cloudflare.accountId` / `apiToken`  |                                          | creates a secret from these |
| `cloudflare.existingSecret`          |                                          | use this secret instead |
| `controller.clusterName`             | `k8s`                                    | prefix of the tunnel names in Cloudflare |
| `controller.tunnelConfigMapNamespace`| release namespace                        | where tunnel ConfigMaps and Secrets live |
| `controller.namespaces`              | `[]` (all)                               | only watch these namespaces |
| `controller.restartDelay`            | `30s`                                    | delay before a crashed cloudflared is restarted |
| `controller.debug`                   | `false`                                  | |
| `controller.extraArgs`               | `[]`                                     | more command line flags |
| `dnsConfig`                          | `{}`                                     | pod DNS config, e.g. `{options: [{name: ndots, value: "2"}]}` |

The chart installs a ClusterRole to read Services, Ingresses and Namespaces and
to manage ConfigMaps and Secrets, plus a Role for the leader election lease.

To let Helm take over resources that were applied by hand, label and annotate
them first:

```sh
for r in deployment/cloudflared-controller serviceaccount/cloudflared-controller \
         clusterrole/cloudflared-controller clusterrolebinding/cloudflared-controller; do
  kubectl -n default label $r app.kubernetes.io/managed-by=Helm --overwrite
  kubectl -n default annotate $r meta.helm.sh/release-name=cloudflared-controller \
    meta.helm.sh/release-namespace=default --overwrite
done
```

### Outside of the cluster

The image is based on `cloudflare/cloudflared`, so it brings its own
`cloudflared`:

```sh
docker run -v $HOME/.kube/config:/home/nonroot/.kube/config \
  -e CLOUDFLARE_ACCOUNT_ID=<account id> \
  -e CLOUDFLARE_API_TOKEN=<api token> \
  -ti ghcr.io/mabels/cloudflared-controller
```

The controller also reads a `.env` file from its working directory.

## Annotations

| Annotation                             | Used on              | Description |
|----------------------------------------|----------------------|-------------|
| `cloudflare.com/tunnel-name`           | Service, Ingress     | Tunnel to use: `name` or `namespace/name`. Without a namespace the tunnel ConfigMap lives in `--cloudflared-tunnel-configmap-namespace`. Several resources can share a tunnel. |
| `cloudflare.com/tunnel-external-name`  | Service              | Public hostname. Required for Services. |
| `cloudflare.com/tunnel-mapping`        | Service, Ingress     | Which ports/hosts to publish, see below. |

The controller itself writes `tunnel-id`, `tunnel-cfd-name`, `tunnel-k8s-secret`
and `tunnel-k8s-configmap` to the tunnel ConfigMap and Secret.

Paths are passed to `cloudflared` as is, so they are regular expressions.

### Services

Without `tunnel-mapping` the controller picks one port, preferring https:
a port named `http`/`https`, else a `targetPort` named `http`/`https`, else a
`targetPort` 80/443. The origin is `<schema>://<service>.<namespace>:<port>`
and the `Host` header is the Service name.

With `tunnel-mapping` you list the ports, comma separated:

```
<port-name>[/<schema>][/<path>|<regex-path>]
```

- `schema` is `http` (default), `https` or `https-notlsverify`
  (https without certificate check)
- `/<path>` adds a leading `/`; `|<path>` takes the path literally, useful for
  regular expressions

```yaml
cloudflare.com/tunnel-mapping: >-
  public/http|^/(assets|favicon|fonts)/,
  next/http/
```

### Ingresses through your ingress controller

For an Ingress of any other class (traefik, nginx, ...) the tunnel points to
that ingress controller, so its TLS, middlewares and routing keep working.
Only rules listed in `tunnel-mapping` are published:

```
<schema>/<ingress-host>/<port>/<host-header>/<public-host>[|<path>]
```

- `ingress-host` is the host of the Ingress rule; it must resolve inside the
  cluster to your ingress controller
- `port` defaults to 80, or 443 if the host is listed under `tls` (which also
  turns `http` into `https`)
- `host-header` defaults to `ingress-host`
- `public-host` is the hostname on Cloudflare
- `path` must equal the path of the Ingress rule, default `/`

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: home-assistant
  annotations:
    cloudflare.com/tunnel-name: home.example.com
    cloudflare.com/tunnel-mapping: https/hass.internal.example.com///hass.example.com
spec:
  ingressClassName: traefik
  rules:
    - host: hass.internal.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: home-assistant
                port:
                  number: 8123
  tls:
    - hosts: [hass.internal.example.com]
      secretName: hass-tls
```

### Ingresses of class `cloudflared` (experimental)

With `ingressClassName: cloudflared` the tunnel goes straight to the backend
Services, the rule hosts are the public hostnames. Without a
`tunnel-name` the tunnel is named after the hosts' domain
(`app.example.com` → `example.com` in the Ingress namespace), so all hosts
must share one domain. Backend ports must be numbers.

`tunnel-mapping` optionally sets schema and host header per rule:

```
<host>[/<schema>[/<host-header>]][|<path>]
```

```yaml
cloudflare.com/tunnel-mapping: >-
  app.example.com/https/app.internal|/api,
  app.example.com/https-notlsverify
```

## Command line flags

All flags have defaults; only the Cloudflare credentials are required.

| Flag | Default | Description |
|------|---------|-------------|
| `-t, --cloudflare-api-token` | env `CLOUDFLARE_API_TOKEN` | |
| `-a, --cloudflare-accountid` | env `CLOUDFLARE_ACCOUNT_ID` | |
| `-u, --cloudflare-api-url` | `https://api.cloudflare.com/client/v4` | also env `CLOUDFLARE_API_URL` |
| `--cloudflared-clustername` | `k8s` | prefix of the tunnel names in Cloudflare |
| `--cloudflared-tunnel-configmap-namespace` | `default` | namespace for tunnel ConfigMaps and Secrets |
| `-C, --config-map-label` | `app=cloudflared-controller` | label of the tunnel ConfigMaps |
| `-n, --namespace` | all | namespace to watch, repeatable |
| `-c, --kubeconfig` | `$HOME/.kube/config` | falls back to in-cluster config |
| `-d, --no-cloudflared` | `false` | manage tunnels and DNS only, don't run cloudflared |
| `--cloudflared-fname` | `cloudflared` | cloudflared binary |
| `-R, --running-instance-dir` | `./` | where cloudflared configs and credentials are written |
| `--restart-delay` | `30s` | delay before a crashed cloudflared is restarted |
| `-i, --identity` | env `POD_NAME`, else random | leader election identity |
| `--leader-name` / `--leader-namespace` | `cloudflared-controller` / `default` | leader election lease |
| `-l, --leader-lease-duration` | `15s` | |
| `-r, --leader-renew-deadline` | `10s` | |
| `-p, --leader-retry-period` | `2s` | |
| `--debug` | `false` | debug logging |
| `--version` | | print the version |
| `--test-create-access` | | experiment: create a Cloudflare Access application |

## Troubleshooting

- `kubectl get cm -l app=cloudflared-controller -A` shows the tunnels and their
  routes; the keys are `<kind>_<namespace>_<name>`.
- `no zone for <host>` in the log: the token can't see the zone of that host,
  check the token's zone resources.
- `403 Forbidden ... Authentication error` on `dns_records`: the token lacks
  *DNS: Edit*; on `cfd_tunnel`: it lacks *Cloudflare Tunnel: Edit*.
- `Can't verify the existing tunnel, not creating a new one`: the token can't
  read tunnels, see [Requirements](#requirements).
- `has an A record, not replacing it`: remove the record in Cloudflare, the
  controller then creates the CNAME.
- `Tunnel exists but its secret is missing or does not match`: the tunnel in
  Cloudflare was created with credentials that are gone. Delete the tunnel in
  Cloudflare, the controller creates a new one.
- `--debug` logs every event and Cloudflare call.

## Development

```sh
go build ./...
go test -race ./...
helm lint helm/cloudflared-controller --set cloudflare.existingSecret=x
```

The tests need no cluster and no Cloudflare account; they use a fake
Kubernetes client, a fake Cloudflare API and a fake `cloudflared`.

| Package | |
|---|---|
| `controller/svc`, `controller/ingress` | turn annotated resources into routes |
| `controller/k8s_data` | tunnel ConfigMaps and Secrets |
| `controller/cloudflared` | tunnels and DNS (leader), running cloudflared (all replicas) |
| `controller/cfclient` | Cloudflare API (cloudflare-go v7) |
| `controller/informer` | thin wrapper around client-go informers |

Releases: CI builds every push. A tag `vX.Y.Z` publishes
`ghcr.io/mabels/cloudflared-controller:X.Y.Z` and `latest`, other pushes publish
`X.Y.Z-pre<commit>-cfd<cloudflared version>` and `dev`. A weekly build
rebuilds the last release with the newest `cloudflared`.

## Ideas

- report the state in the resources' status instead of logs only
- Cloudflare Access applications per hostname
- an `IngressClass` object in the Helm chart
