# cloudflared-controller
An Kubernets Ingress/Service Controller for cloudflared

this is very much a work in progress

## TODO
- implemented in production to see if it is stable
- helm chart (inkl rbac to control that configmap und secrets are writeable/readable)
- add ingressClass support
- add service support
- improve the documentation
- add "more" tests

## How to use
Set the following environment variables
```
CLOUDFLARE_ZONE_ID=<from your CF website>
CLOUDFLARE_API_TOKEN=<from your CF Console>
CLOUDFLARE_ACCOUNT_ID=<from your CF website>
```

## Docker
```sh
docker run -v $HOME/.kube/config:/home/nonroot/.kube/config \
   -e CLOUDFLARE_ACCOUNT_ID=<from your CF website> \
   -e CLOUDFLARE_API_TOKEN=<from your CF Console> \
   -e CLOUDFLARE_ZONE_ID=<from your CF website> \
   -ti ghcr.io/mabels/cloudflared-controller
```

## Sample ingress to make it work we need these both annotations
```
metadata:
  annotations:
    cloudflare.com/tunnel-external-name: ha.cloudflare-website.domain
    cloudflare.com/tunnel-name: cloudflare-website.domain
```

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  annotations:
    cloudflare.com/tunnel-external-name: ha.cloudflare-website.domain
    cloudflare.com/tunnel-name: cloudflare-website.domain
    external-dns.alpha.kubernetes.io/target: 192.168.99.99
    external-dns.alpha.kubernetes.io/ttl: "60"
    traefik.ingress.kubernetes.io/redirect-entry-point: https
    traefik.ingress.kubernetes.io/router.entrypoints: websecure
    traefik.ingress.kubernetes.io/router.tls: "true"
  generation: 1
  labels:
    app.kubernetes.io/instance: home-assistant
    app.kubernetes.io/name: home-assistant
    app.kubernetes.io/version: 2022.5.4
  name: home-assistant
  namespace: default
spec:
  ingressClassName: traefik
  rules:
  - host: hass-io-wl.internal.domain
    http:
      paths:
      - backend:
          service:
            name: home-assistant
            port:
              number: 8123
        path: /
        pathType: Prefix
  tls:
  - hosts:
    - hass-io-wl.internal.domain
    secretName: hass-io-wl
status:
  loadBalancer: {}
```
