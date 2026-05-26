# api-skill-trail

## Local ECR push

Create a local `.env` file from the example:

```sh
cp .env.example .env
```

Set these values in `.env`:

```env
AWS_REGION=ap-northeast-1
AWS_PROFILE=your-profile-name
ECR_REPOSITORY_URL=123456789012.dkr.ecr.ap-northeast-1.amazonaws.com/backend-skill-trail
IMAGE_TAG=local
DOCKER_PLATFORM=linux/arm64
```

Then build and push the Docker image:

```sh
./scripts/push-image.sh
```

The script prints the pushed image URI after `docker push` succeeds.

## Local ecspresso render

Set the ECS values in `.env`.

Required ECS values:

```env
ECS_CLUSTER_NAME=your-cluster-name
ECS_SERVICE_NAME=backend-skill-trail
CONTAINER_NAME=app
CONTAINER_PORT=8080
TASK_EXECUTION_ROLE_ARN=arn:aws:iam::123456789012:role/ecsTaskExecutionRole
TASK_ROLE_ARN=arn:aws:iam::123456789012:role/ecsTaskRole
SUBNET_IDS_JSON='["subnet-xxxxxxxx","subnet-yyyyyyyy"]'
SECURITY_GROUP_IDS_JSON='["sg-xxxxxxxx"]'
ASSIGN_PUBLIC_IP=DISABLED
```

`SUBNET_IDS_JSON` and `SECURITY_GROUP_IDS_JSON` must be JSON arrays wrapped in single quotes because `.env` is loaded by the shell.

Render the ecspresso files:

```sh
./scripts/ecspresso.sh render config
./scripts/ecspresso.sh render task-definition
./scripts/ecspresso.sh render service-definition
```

Compare with the current ECS state:

```sh
./scripts/ecspresso.sh diff
```

Deploy the service:

```sh
./scripts/ecspresso.sh deploy
```

In ecspresso v2, `deploy` creates the ECS service when it does not exist yet.
