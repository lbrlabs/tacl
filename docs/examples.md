# Examples

Below are **curl** examples (using `GET`, `POST`, `PUT`, and `DELETE`) for each major endpoint group defined in your code.  
All examples assume your Tacl server is running on `http://tacl:8080`. Adjust host/port as needed.

---

## 1. **ACLs** – `/acls`

### List All ACLs
```bash
curl -X GET http://tacl:8080/acls
```
**Response**: Returns an array of ACL objects, each of which has:
```json
[
  {
    "id": "<UUID>",
    "action": "...",
    "src": [...],
    "dst": [...],
    ...
  },
  ...
]
```

### Get a Single ACL by ID
```bash
curl -X GET http://tacl:8080/acls/<ACL_ID>
```
Replace `<ACL_ID>` with the UUID of the ACL you want to retrieve.

### Create a New ACL (POST)
```bash
curl -X POST http://tacl:8080/acls \
  -H "Content-Type: application/json" \
  -d '{
    "action": "accept",
    "src": ["mail@lbrlabs.com"],
    "dst": ["*:*"]
  }'
```
**Response**: Returns the newly created ACL, including a generated `id`.

### Update an Existing ACL (PUT)
```bash
curl -X PUT http://tacl:8080/acls \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<ACL_ID>",
    "entry": {
      "action": "accept",
      "src": ["mail@lbrlabs.com","newuser@example.com"],
      "dst": ["*:*"]
    }
  }'
```
Replace `<ACL_ID>` with the UUID of the ACL you want to update.

### Delete an ACL (DELETE)
```bash
curl -X DELETE http://tacl:8080/acls \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<ACL_ID>"
  }'
```
Replace `<ACL_ID>` with the UUID of the ACL you want to delete.

---

## 2. **ACLTests** – `/acltests`

ACLTests are now stored and looked up by a **stable UUID**, rather than array indices.

### List All ACLTests
```bash
curl -X GET http://tacl:8080/acltests
```
**Response**: Returns an array of objects, each with:
```json
[
  {
    "id": "<UUID>",
    "src": "...",
    "dst": "...",
    "allow": true/false
  },
  ...
]
```
*(These fields mirror Tailscale’s `ACLTest`, plus an `id`.)*

### Get One ACLTest by ID
```bash
curl -X GET http://tacl:8080/acltests/<UUID>
```
Replace `<UUID>` with the test’s `id`.

### Create a New ACLTest (POST)
```bash
curl -X POST http://tacl:8080/acltests \
  -H "Content-Type: application/json" \
  -d '{
    "src": "user@example.com",
    "dst": "tag:server:22",
    "allow": true
  }'
```
**Response**: Returns the newly created test object, including a generated `id`.

### Update an Existing ACLTest (PUT)
```bash
curl -X PUT http://tacl:8080/acltests \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>",
    "test": {
      "src": "user@example.com",
      "dst": "tag:server:22",
      "allow": false
    }
  }'
```
Replace `<UUID>` with the `id` of the test you want to update.

### Delete an ACLTest (DELETE)
```bash
curl -X DELETE http://tacl:8080/acltests \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>"
  }'
```
Replace `<UUID>` with the `id` of the test you want to remove.

---

## 3. **AutoApprovers** – `/autoapprovers`

These endpoints manage a **single** `ACLAutoApprovers` object (not an array).

### Get AutoApprovers
```bash
curl -X GET http://tacl:8080/autoapprovers
```
**Response**: Returns the current `ACLAutoApprovers` object, or `404` if none.

### Create AutoApprovers (POST)
```bash
curl -X POST http://tacl:8080/autoapprovers \
  -H "Content-Type: application/json" \
  -d '{
    "routes": {
      "0.0.0.0/0": ["tag:router"]
    },
    "exitNode": ["tag:router"]
  }'
```
**Note**: If an AutoApprovers object already exists, this endpoint may return `409 Conflict`.

### Update AutoApprovers (PUT)
```bash
curl -X PUT http://tacl:8080/autoapprovers \
  -H "Content-Type: application/json" \
  -d '{
    "routes": {
      "10.0.0.0/24": ["tag:router"]
    },
    "exitNode": ["tag:router"]
  }'
```
If there’s no existing object, this might return `404`.

---

## 4. **DERPMap** – `/derpmap`

Manages a single `ACLDERPMap` object.

### Get the DERPMap
```bash
curl -X GET http://tacl:8080/derpmap
```
**Response**: The current DERPMap object, or `404` if none is set.

### Create a New DERPMap (POST)
```bash
curl -X POST http://tacl:8080/derpmap \
  -H "Content-Type: application/json" \
  -d '{
    "Regions": {
      "1": {
        "RegionID": 1,
        "RegionCode": "nyc",
        "Nodes": [{
          "Name": "nyc1.example.com",
          "RegionID": 1,
          "HostName": "nyc1.example.com"
        }]
      }
    }
  }'
```

### Update Existing DERPMap (PUT)
```bash
curl -X PUT http://tacl:8080/derpmap \
  -H "Content-Type: application/json" \
  -d '{
    "Regions": {
      "1": {
        "RegionID": 1,
        "RegionCode": "nyc",
        "Nodes": [{
          "Name": "nyc1.example.com",
          "RegionID": 1,
          "HostName": "nyc1.example.com"
        }],
        "STUNOnly": true
      }
    }
  }'
```

---

## 5. **Groups** – `/groups`

### List All Groups
```bash
curl -X GET http://tacl:8080/groups
```
**Response**: Returns an array of group objects:  
```json
[
  { "name": "groupname", "members": ["alice@example.com", ...] },
  ...
]
```

### Get a Group by Name
```bash
curl -X GET http://tacl:8080/groups/<NAME>
```
Replace `<NAME>` with the group’s name.

### Create a New Group (POST)
```bash
curl -X POST http://tacl:8080/groups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "engineering",
    "members": ["alice@example.com", "bob@example.com"]
  }'
```
**Response**: Returns the newly created group.

### Update an Existing Group (PUT)
```bash
curl -X PUT http://tacl:8080/groups \
  -H "Content-Type: application/json" \
  -d '{
    "name": "engineering",
    "members": ["alice@example.com", "charlie@example.com"]
  }'
```
**Response**: Returns the updated group object.

---

## 6. **Hosts** – `/hosts`

### List All Hosts
```bash
curl -X GET http://tacl:8080/hosts
```
**Response**: Returns an array of host objects:
```json
[
  { "name": "hostname", "ip": "10.0.0.1" },
  ...
]
```

### Get a Host by Name
```bash
curl -X GET http://tacl:8080/hosts/<HOST_NAME>
```
Replace `<HOST_NAME>` with the host’s name.

### Create a New Host (POST)
```bash
curl -X POST http://tacl:8080/hosts \
  -H "Content-Type: application/json" \
  -d '{
    "name": "example-host-1",
    "ip": "10.1.2.3"
  }'
```

### Update an Existing Host (PUT)
```bash
curl -X PUT http://tacl:8080/hosts \
  -H "Content-Type: application/json" \
  -d '{
    "name": "example-host-1",
    "ip": "10.1.2.100"
  }'
```

---

## 7. **Node Attributes** – `/nodeattrs`

### List All Node Attribute Grants
```bash
curl -X GET http://tacl:8080/nodeattrs
```
**Response**: Returns an array of `ExtendedNodeAttrGrant` objects:
```json
[
  {
    "id": "<UUID>",
    "target": [...],
    "attr": [...],
    "app": {...},
    ...
  },
  ...
]
```

### Get a Single Node Attr by ID
```bash
curl -X GET http://tacl:8080/nodeattrs/<UUID>
```
Replace `<UUID>` with the grant’s `id`.

### Create a New Node Attr (POST)
```bash
curl -X POST http://tacl:8080/nodeattrs \
  -H "Content-Type: application/json" \
  -d '{
    "target": ["group:engineering"],
    "attr": ["test=example"]
  }'
```
**Or** (for an app-based node attribute):
```bash
curl -X POST http://tacl:8080/nodeattrs \
  -H "Content-Type: application/json" \
  -d '{
    "app": {
      "myapp.example.com": [
        {
          "name": "server",
          "connectors": ["dbconnector"],
          "domains": ["example.com"]
        }
      ]
    }
  }'
```
Exactly one of `attr` or `app` must be set.

### Update an Existing Node Attr (PUT)
```bash
curl -X PUT http://tacl:8080/nodeattrs \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>",
    "grant": {
      "target": ["group:engineering"],
      "attr": ["updated=rules"]
    }
  }'
```
Replace `<UUID>` with the `id` of the node attribute to update.

### Delete a Node Attr (DELETE)
```bash
curl -X DELETE http://tacl:8080/nodeattrs \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>"
  }'
```
Replace `<UUID>` with the `id` of the node attribute to remove.

---

## 8. **Postures** – `/postures`

### 8.1. Named Postures

#### List All Postures
```bash
curl -X GET http://tacl:8080/postures
```
**Response**:
```json
{
  "defaultSourcePosture": [...],
  "items": [
    { "name": "latestMac", "rules": ["node:os == 'macos'"] },
    ...
  ]
}
```

#### Get a Single Named Posture
```bash
curl -X GET http://tacl:8080/postures/<NAME>
```
*(If `<NAME>` is `default`, see **8.2** below.)*

#### Create a New Named Posture (POST)
```bash
curl -X POST http://tacl:8080/postures \
  -H "Content-Type: application/json" \
  -d '{
    "name": "latestMac",
    "rules": ["node:os == \"macos\"", "node:tsVersion >= \"1.40\""]
  }'
```

#### Update a Named Posture (PUT)
```bash
curl -X PUT http://tacl:8080/postures \
  -H "Content-Type: application/json" \
  -d '{
    "name": "latestMac",
    "rules": ["node:os == \"macos\"", "node:tsVersion >= \"1.42\""]
  }'
```

### 8.2. Default Posture – `/postures/default`

#### Get Default Posture
```bash
curl -X GET http://tacl:8080/postures/default
```
**Response**:  
```json
{
  "defaultSourcePosture": [...],
  "items": [...]
}
```
(Or `404` if no default posture is set.)

#### Set/Update Default Posture (PUT)
```bash
curl -X PUT http://tacl:8080/postures/default \
  -H "Content-Type: application/json" \
  -d '{
    "defaultSourcePosture": [
      "node:os == \"linux\"",
      "node:tsVersion >= \"1.40\""
    ]
  }'
```

---

## 9. **Settings** – `/settings`

### Get Current Settings
```bash
curl -X GET http://tacl:8080/settings
```
**Response**: Returns the JSON settings object, or `404` if none are set.

### Create New Settings (POST)
```bash
curl -X POST http://tacl:8080/settings \
  -H "Content-Type: application/json" \
  -d '{
    "disableIPv4": false,
    "oneCGNATRoute": "100.64.0.0/10",
    "randomizeClientPort": true
  }'
```
If settings already exist, may return `409 Conflict`.

### Update Existing Settings (PUT)
```bash
curl -X PUT http://tacl:8080/settings \
  -H "Content-Type: application/json" \
  -d '{
    "disableIPv4": true,
    "oneCGNATRoute": "100.64.0.0/10",
    "randomizeClientPort": false
  }'
```
If none exist yet, might return `404`.

---

## 10. **SSH Rules** – `/ssh`

SSH rules are now managed by **UUID** IDs (similar to ACL entries).

### List All SSH Rules
```bash
curl -X GET http://tacl:8080/ssh
```
**Response**: Returns an array of objects:
```json
[
  {
    "id": "<UUID>",
    "action": "accept",
    "src": [...],
    "dst": [...],
    "users": [...],
    "checkPeriod": "...",
    "acceptEnv": [...]
  },
  ...
]
```

### Get a Single SSH Rule by ID
```bash
curl -X GET http://tacl:8080/ssh/<UUID>
```
Replace `<UUID>` with the rule’s `id`.

### Create a New SSH Rule (POST)
```bash
curl -X POST http://tacl:8080/ssh \
  -H "Content-Type: application/json" \
  -d '{
    "action": "accept",
    "src": ["alice@example.com"],
    "dst": ["tag:production"],
    "users": ["root","devops"]
  }'
```
Or a `check`-type rule with a `checkPeriod`:
```bash
curl -X POST http://tacl:8080/ssh \
  -H "Content-Type: application/json" \
  -d '{
    "action": "check",
    "checkPeriod": "12h",
    "src": ["alice@example.com"],
    "dst": ["tag:production"],
    "users": ["root","devops"]
  }'
```
**Response**: Returns the newly created rule, including its `id`.

### Update an Existing SSH Rule (PUT)
```bash
curl -X PUT http://tacl:8080/ssh \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>",
    "rule": {
      "action": "check",
      "checkPeriod": "24h",
      "src": ["alice@example.com"],
      "dst": ["tag:production"],
      "users": ["root","devops"],
      "acceptEnv": ["FOO", "BAR"]
    }
  }'
```
Replace `<UUID>` with the rule’s `id`.

### Delete an SSH Rule (DELETE)
```bash
curl -X DELETE http://tacl:8080/ssh \
  -H "Content-Type: application/json" \
  -d '{
    "id": "<UUID>"
  }'
```
Replace `<UUID>` with the rule’s `id`.

---

## 11. **Tag Owners** – `/tagowners`

### List All Tag Owners
```bash
curl -X GET http://tacl:8080/tagowners
```
**Response**: Returns an array of tag-owner objects:
```json
[
  { "name": "webserver", "owners": ["autogroup:admin"] },
  ...
]
```

### Get a Single TagOwner by Name
```bash
curl -X GET http://tacl:8080/tagowners/<NAME>
```
Replace `<NAME>` with the tag name, e.g. `"webserver"`.

### Create a New TagOwner (POST)
```bash
curl -X POST http://tacl:8080/tagowners \
  -H "Content-Type: application/json" \
  -d '{
    "name": "webserver",
    "owners": ["autogroup:admin", "alice@example.com"]
  }'
```
**Response**: Returns the created object.

### Update an Existing TagOwner (PUT)
```bash
curl -X PUT http://tacl:8080/tagowners \
  -H "Content-Type: application/json" \
  -d '{
    "name": "webserver",
    "owners": ["autogroup:admin", "bob@example.com"]
  }'
```
Replace `"webserver"` with whatever tag name you’re updating.