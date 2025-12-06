#!/bin/bash
# Fix webhook caBundle issue
# Usage: ./fix-webhook-cabundle.sh

set -e

NAMESPACE="operators"
SECRET_NAME="hostport-operator-webhook-cert"
WEBHOOK_CONFIG_NAME="hostport-operator-hostport-mutating-webhook-configuration"

echo "==> Extracting caBundle from Secret..."
CA_BUNDLE=$(kubectl get secret ${SECRET_NAME} -n ${NAMESPACE} -o jsonpath='{.data.tls\.crt}')

if [ -z "${CA_BUNDLE}" ]; then
    echo "❌ Error: Failed to extract caBundle from Secret ${SECRET_NAME}"
    exit 1
fi

echo "==> Updating MutatingWebhookConfiguration with caBundle..."
# Try to add caBundle (will fail if already exists, then try replace)
kubectl patch mutatingwebhookconfiguration ${WEBHOOK_CONFIG_NAME} \
  --type='json' \
  -p="[{\"op\": \"add\", \"path\": \"/webhooks/0/clientConfig/caBundle\", \"value\": \"${CA_BUNDLE}\"}]" 2>/dev/null || \
kubectl patch mutatingwebhookconfiguration ${WEBHOOK_CONFIG_NAME} \
  --type='json' \
  -p="[{\"op\": \"replace\", \"path\": \"/webhooks/0/clientConfig/caBundle\", \"value\": \"${CA_BUNDLE}\"}]"

if [ $? -eq 0 ]; then
    echo "✅ caBundle updated successfully"
    echo ""
    echo "==> Verifying caBundle..."
    VERIFIED=$(kubectl get mutatingwebhookconfiguration ${WEBHOOK_CONFIG_NAME} -o jsonpath='{.webhooks[0].clientConfig.caBundle}')
    if [ -n "${VERIFIED}" ]; then
        echo "✅ caBundle is set correctly (length: ${#VERIFIED} characters)"
    else
        echo "⚠️  Warning: caBundle may not be set correctly"
    fi
else
    echo "❌ Error: Failed to update caBundle"
    exit 1
fi

