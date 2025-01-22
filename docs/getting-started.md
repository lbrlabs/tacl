# Getting Started

You can initialize a basic working ACL with `tacl init`. This will bootstrap a default, permissive ACL that allows access to Tacl, sets up a tag and tagowner and syncs it to your state store:

```json
{
    "acls": [
      {
        "action": "accept",
        "dst": [
          "*:*"
        ],
        "src": [
          "*"
        ]
      }
    ],
    "autoApprovers": {},
    "grants": [
      {
        "app": {
          "lbrlabs.com/cap/tacl": [
            {
              "manager": {
                "endpoints": [
                  "*"
                ],
                "methods": [
                  "*"
                ]
              }
            }
          ]
        },
        "dst": [
          "tag:tacl"
        ],
        "src": [
          "autogroup:admin"
        ]
      }
    ],
    "ssh": [],
    "tagOwners": {
      "tag:tacl": [
        "autogroup:admin"
      ]
    }
  }
  
```

Tacl requires a Tailscale oauth client with the `auth_keys` write scope and the `policy_file` scope. From there, you can run it like so:

```bash
tacl serve --client-id=<client-id>> --client-secret=<client-secret> --tailnet <tailnet-name>
```