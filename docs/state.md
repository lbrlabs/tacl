# State

Tacl maintains a state file of your Tailscale ACL, which it periodically syncs to your Tailnet. You can use local file state, or an S3 compatible object store.

### Local File State

You can use a local file for state easily like so:

```bash
tacl serve --client-id=<your-client-id> --client-secret=<your-client-secret> --tailnet-name <your-tailnet> --storage=file://state.json
```

### S3 State

If you'd like to store state in S3, simply use an S3 prefix:

```bash
tacl server --client-id=<your-client-id> --client-secret=<your-client-secret> --tailnet-name <your-tailnet> --storage=s3://lbriggs-tacl/state.json
```

You can use s3 compatible endpoints as well, see the `--s3-endpoint="s3.amazonaws.com"` and `--s3-region="us-east-1"` flags and corresponding environment variables.