#!/bin/sh

# Create the configs directory if it doesn't exist
mkdir -p /app/configs

# Check if the configs directory is empty and copy the default config if it is
if [ ! "$(ls -A /app/configs)" ]; then
  echo "Configs directory is empty, initializing with default config..."
  mv /app/config.yml.default /app/configs/config.yml
fi

# Run the main application
exec ./fusionn
