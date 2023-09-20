ARG  CLOUDFLARE_CLOUDFLARED_VERSION=latest
FROM cloudflare/cloudflared:${CLOUDFLARE_CLOUDFLARED_VERSION}
#FROM alpine:latest

#RUN ls -l
COPY ./cloudflared-controller /bin/cloudflared-controller

ENTRYPOINT ["cloudflared-controller"]
