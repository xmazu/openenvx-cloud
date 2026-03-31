FROM oven/bun:alpine as builder

WORKDIR /app
COPY . .
RUN bun install
WORKDIR /app/apps/terraform-worker
RUN bun build ./src/index.ts --compile --outfile=worker

FROM alpine:latest

# Added libstdc++ and libgcc for bun standalone binary compatibility
RUN apk add --no-cache curl unzip bash ca-certificates jq gcompat libstdc++ libgcc

ENV TERRAFORM_VERSION="1.8.3"
RUN curl -sLO "https://releases.hashicorp.com/terraform/${TERRAFORM_VERSION}/terraform_${TERRAFORM_VERSION}_linux_amd64.zip" && \
    unzip "terraform_${TERRAFORM_VERSION}_linux_amd64.zip" -d /usr/local/bin/ && \
    rm "terraform_${TERRAFORM_VERSION}_linux_amd64.zip" && \
    chmod +x /usr/local/bin/terraform

RUN curl -1sLf 'https://dl.cloudsmith.io/public/infisical/infisical-cli/setup.alpine.sh' | bash && \
    apk add --no-cache infisical

COPY --from=builder /app/apps/terraform-worker/worker /usr/local/bin/terraform-worker
RUN chmod +x /usr/local/bin/terraform-worker

CMD ["/bin/bash"]
