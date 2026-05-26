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
