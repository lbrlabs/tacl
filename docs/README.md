# Tacl

Tacl is an **experimental** tool that enables management of Tailscale ACLs via a CRUD based API, instead of a single flat file.

> [!WARNING] 
> Tacl is not production ready. Please don't use it to manage your production Tailscale ACL yet.


It works by maintaining a state file, and then periodically syncing that file to the Tailscale API. You send requests to Tacl, and it appends JSON to a final JSON state, meaning you can add more granular components to a Tailscale ACL.

## Example

Let's say we have a very basic Tailscale ACL, that only contains the default permissive ACL block:

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
  "autoApprovers": {
    "exitNode": [
      "tag:router"
    ],
    "routes": {
      "0.0.0.0/0": [
        "tag:router"
      ]
    }
  },
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
    "tag:router": [
      "autogroup:admin"
    ],
    "tag:tacl": [
      "autogroup:admin"
    ]
  }
}

```

And I want to add something to it. Tailscale has an ACL editor which would allow me to do that. However, with Tacl, I can add an ACL with an API, so let's go ahead and do that:

```bash
curl -X POST http://tacl:8080/acls -H "Content-Type: application/json" -d '{"action": "accept", "dst": ["*:*
"], "src": ["mail@lbrlabs.com"]}'    
```

Tacl handles appending this new ACL to my final state:

```json
{
  "acls": [
    {
      "id": "",
      "action": "accept",
      "src": [
        "*"
      ],
      "dst": [
        "*:*"
      ]
    },
    {
      "id": "9fa3cb21-221d-4415-8123-e36625ea1282",
      "action": "accept",
      "src": [
        "mail@lbrlabs.com"
      ],
      "dst": [
        "*:*"
      ]
    }
  ],
  "autoApprovers": {
    "exitNode": [
      "tag:router"
    ],
    "routes": {
      "0.0.0.0/0": [
        "tag:router"
      ]
    }
  },
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
    "tag:router": [
      "autogroup:admin"
    ],
    "tag:tacl": [
      "autogroup:admin"
    ]
  }
}

```

> [!NOTE]
> The ID field is unique to some parts of the ACL, and is not synced to the resulting ACL, it's used to manage array based elements.

Now, if I check my Tacl logs:

```json
{"level":"info","ts":1737502379.9816508,"caller":"cap/cap.go:69","msg":"Incoming request from Tailscale","ip":"100.84.60.2","userLoginName":"mail@lbrlabs.com","displayName":"mail","method":"POST","url":"/acls"}
{"level":"info","ts":1737502379.981961,"caller":"zap@v1.1.4/zap.go:117","msg":"/acls","status":201,"method":"POST","path":"/acls","query":"","ip":"100.84.60.2","user-agent":"curl/8.7.1","latency":0.000267042,"time":"2025-01-21T23:32:59Z"}
{"level":"info","ts":1737502411.551996,"caller":"sync/sync.go:60","msg":"Pushed local ACL to Tailscale","bytes":916}
```

I've now managed my ACL without having to manually update the file, Tacl synced it to my Tailnet. I can of course also `DELETE` or `PUT` to an entry to update it.

There are endpoints for almost all resources in Tailscale, so if you want to add a `tagOwner`, `autoApprover` or `grant` - you can do so! See [examples](examples.md) for more information!


## Authentication

Tacl leverages Tailscale's built in application capabilities, so you'll need to have the following in your ACL:

```
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
}],
```

You can specify the rest endpoints like `/acls` or `postures` to allow who's able to send requests, and specify methods like `GET` or `POST`. In order to communicate with tacl, you'll need a Tailscale client in the `src`.

## State

Tacl stores an intermediary state either in a local file or object store, which it syncs to Tailscale peridiocally. The state is not a valid Tailscale ACL, as Tacl adds some ID fields (which it strips out before syncing) to certain parts of the state in order to be able to effectively manage ACLs.

See (state.md)



## Limitations

- Tacl expects to be the source of truth for your ACL file.
- Tacl does not currently support hujson, so you'll need to convert your existing ACL to plain JSON.
