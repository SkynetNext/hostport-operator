# Cert-Manager Configuration
# 
# Prerequisites:
# 1. cert-manager must be installed in the cluster
# 2. Create the ClusterIssuer (one-time setup):
#    kubectl apply -f config/certmanager/clusterissuer.yaml
#
# The Certificate resource will automatically generate and renew the TLS certificate
# for the webhook service, eliminating the need for manual certificate management.

