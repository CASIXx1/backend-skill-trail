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
TFSTATE_URL=s3://your-terraform-state-bucket/path/to/terraform.tfstate
ECS_CLUSTER_NAME=your-cluster-name
ECS_SERVICE_NAME=backend-skill-trail
CONTAINER_NAME=app
CONTAINER_PORT=8080
ASSIGN_PUBLIC_IP=DISABLED
```

The task definition and service definition resolve IAM roles, ECR repository URL, private subnet IDs, ECS task security group ID, and service name from `TFSTATE_URL`.

Render the ecspresso files:

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml render config
ecspresso --envfile .env --config ecspresso/ecspresso.yml render task-definition
ecspresso --envfile .env --config ecspresso/ecspresso.yml render service-definition
```

Compare with the current ECS state:

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml diff
```

Deploy the service:

```sh
ecspresso --envfile .env --config ecspresso/ecspresso.yml deploy
```

In ecspresso v2, `deploy` creates the ECS service when it does not exist yet.
