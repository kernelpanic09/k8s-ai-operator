# IRSA Setup

The operator authenticates to AWS using IAM Roles for Service Accounts (IRSA).
You need two IAM roles:

1. **Operator role** - the role the operator's own pod assumes. Needs `sts:AssumeRole` on your per-endpoint roles.
2. **Per-endpoint role(s)** - one per ModelEndpoint (or shared across endpoints in the same account/region). Needs `bedrock:InvokeModel` on the relevant model ARNs.

## Create the operator role

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
CLUSTER_NAME=my-cluster
REGION=us-east-1
OIDC_ISSUER=$(aws eks describe-cluster \
  --name $CLUSTER_NAME \
  --query "cluster.identity.oidc.issuer" \
  --output text | sed 's|https://||')

# Trust policy: allows the operator's ServiceAccount to assume this role.
cat > /tmp/operator-trust.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::${ACCOUNT_ID}:oidc-provider/${OIDC_ISSUER}"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "${OIDC_ISSUER}:sub": "system:serviceaccount:ai-operator-system:k8s-ai-operator",
        "${OIDC_ISSUER}:aud": "sts.amazonaws.com"
      }
    }
  }]
}
EOF

aws iam create-role \
  --role-name k8s-ai-operator \
  --assume-role-policy-document file:///tmp/operator-trust.json

# Allow the operator to assume per-endpoint roles.
aws iam put-role-policy \
  --role-name k8s-ai-operator \
  --policy-name assume-endpoint-roles \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": "sts:AssumeRole",
      "Resource": "arn:aws:iam::'"${ACCOUNT_ID}"':role/bedrock-invoker-*"
    }]
  }'
```

## Create a per-endpoint role

```bash
cat > /tmp/endpoint-trust.json <<EOF
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "AWS": "arn:aws:iam::${ACCOUNT_ID}:role/k8s-ai-operator"
    },
    "Action": "sts:AssumeRole"
  }]
}
EOF

aws iam create-role \
  --role-name bedrock-invoker-prod \
  --assume-role-policy-document file:///tmp/endpoint-trust.json

aws iam put-role-policy \
  --role-name bedrock-invoker-prod \
  --policy-name invoke-bedrock \
  --policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Action": [
        "bedrock:InvokeModel",
        "bedrock:GetFoundationModel"
      ],
      "Resource": "*"
    }]
  }'
```

## Annotate the ServiceAccount

Patch `config/rbac/service_account.yaml` with the operator role ARN before deploying:

```yaml
metadata:
  annotations:
    eks.amazonaws.com/role-arn: arn:aws:iam::123456789012:role/k8s-ai-operator
```

Or with kubectl after deploying:

```bash
kubectl annotate serviceaccount k8s-ai-operator \
  -n ai-operator-system \
  eks.amazonaws.com/role-arn=arn:aws:iam::${ACCOUNT_ID}:role/k8s-ai-operator
```

## Verify

```bash
# Confirm the operator pod has the right environment variables injected.
kubectl exec -n ai-operator-system deploy/k8s-ai-operator -- env | grep AWS
# Should show: AWS_ROLE_ARN, AWS_WEB_IDENTITY_TOKEN_FILE, AWS_REGION
```
