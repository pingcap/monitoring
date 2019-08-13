#!/bin/bash

# set helm release names
if [[ ! -z $1 ]]; then
  RELEASE_NAME=$1
else
  echo "No release name specified, do nothing."
  exit 0
fi

find charts/ -name "*.yaml" | xargs sed -i "s/infra/$RELEASE_NAME/g"
find manifests/ -name "*.yaml" | xargs sed -i "s/infra/$RELEASE_NAME/g"
sed -i "s/infra/$RELEASE_NAME/g" install-logging.sh
