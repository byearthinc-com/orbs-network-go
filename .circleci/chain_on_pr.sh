#!/bin/bash -e

touch $BASH_ENV
curl -o- https://raw.githubusercontent.com/creationix/nvm/v0.33.11/install.sh | bash
export NVM_DIR="/opt/circleci/.nvm" && . $NVM_DIR/nvm.sh && nvm install v10.14.1 && nvm use v10.14.1

COMMIT_HASH=$(./docker/hash.sh)

# Determine if we have an active PR
if [ ! -z "$CI_PULL_REQUESTS" ]
then
    echo "We have an active PR ($CI_PULL_REQUESTS)"
    curl -O https://boyar-testnet-bootstrap.s3-us-west-2.amazonaws.com/boyar/config.json
    PR_CHAIN_ID=$(node .circleci/testnet-deploy-new-chain-for-pr.js $CI_PULL_REQUESTS $COMMIT_HASH)

    aws s3 cp --acl public-read config.json s3://boyar-testnet-bootstrap/boyar/config.json

    echo "Configuration updated, waiting for the new PR chain ($PR_CHAIN_ID) to come up!"

    sleep 20

    node .circleci/check-testnet-deployment.js

    export API_ENDPOINT=http://35.172.102.63/vchains/$PR_CHAIN_ID/ \
        STRESS_TEST_NUMBER_OF_TRANSACTIONS=100 \
        STRESS_TEST_FAILURE_RATE=20 \
        STRESS_TEST_TARGET_TPS=200 \
        STRESS_TEST=true \

    go test ./test/e2e/... -v
else
    echo "No active PR, exiting.."
fi

exit 0