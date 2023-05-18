# cloudflared-controller
An Kubernets Ingress/Service Controller for cloudflared

this is very much a work in progress

## TODO
- implemented in production to see if it is stable (On going)
- helm chart (inkl rbac to control that configmap und secrets are writeable/readable)
- add ingressClass support (untested)
- improve the documentation (on going)
- add "more" tests (on going)
- restart logic for the cloudflared
- switch watcher to use informer
- runtime addressable name from sha256 of the configmap data
- enable access-control

- add service support (done)
- queue updates for configMap (done)
- state improvements (done)

## How to use
Set the following environment variables
```
CLOUDFLARE_API_TOKEN=<from your CF Console>
CLOUDFLARE_ACCOUNT_ID=<from your CF website>
```
The CLOUDFLARE_API_TOKEN needs the following permissions
- All accounts - Cloudflare Tunnel:Edit
- All zones - DNS:Read


## Docker
```sh
docker run -v $HOME/.kube/config:/home/nonroot/.kube/config \
   -e CLOUDFLARE_ACCOUNT_ID=<from your CF website> \
   -e CLOUDFLARE_API_TOKEN=<from your CF Console> \
   -ti ghcr.io/mabels/cloudflared-controller
```

## Sample basic configuration
```
metadata:
  annotations:
    cloudflare.com/tunnel-external-name: ha.cloudflare-website.domain
    cloudflare.com/tunnel-name: cloudflare-website.domain
```

### Sample service
```yaml
apiVersion: v1
kind: Service
metadata:
  annotations:
    meta.helm.sh/release-name: syncthing
    meta.helm.sh/release-namespace: default
    cloudflare.com/tunnel-external-name: stg.cloudflare-website.domain
    cloudflare.com/tunnel-name: cloudflare-website.domain
  labels:
    app.kubernetes.io/instance: syncthing
  name: syncthing
  namespace: default
spec:
  clusterIP: 10.43.94.217
  clusterIPs:
  - 10.43.94.217
  internalTrafficPolicy: Cluster
  ipFamilies:
  - IPv4
  ipFamilyPolicy: SingleStack
  ports:
  - name: http
    port: 8384
    protocol: TCP
    targetPort: http
  type: ClusterIP
```

### Sample ingress
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

Missing Sample for IngressClass "cloudfared"
