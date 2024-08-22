FROM ubuntu:latest

# Copy entry point script
COPY docker-entrypoint.sh /usr/local/bin/
RUN ln -s /usr/local/bin/docker-entrypoint.sh /entrypoint.sh # backwards compatibility

# Set user and working directory
USER root
WORKDIR /root

# Set the entry point for the container
ENTRYPOINT ["/entrypoint.sh"]
