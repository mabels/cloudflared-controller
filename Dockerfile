FROM cloudflare/cloudflared:2023.5.0

COPY ./cloudflared-controller /bin/cloudflared-controller

ENTRYPOINT ["cloudflared-controller"]
