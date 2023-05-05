FROM cloudflare/cloudflared:2023.5.0
#FROM alpine:latest

#RUN ls -l
COPY ./cloudflared-controller /bin/cloudflared-controller

ENTRYPOINT ["cloudflared-controller"]
