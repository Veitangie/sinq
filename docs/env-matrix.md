# Environment Matrix Mechanism

`env_matrix` runs one scenario multiple times, once per combination of a set of environment variables (a Cartesian product). It lets you run the same workflow against different data without copying `.sinq` files or directories.

## Definition and Cartesian Product

`env_matrix` takes an array of objects, one per axis. In each axis, the keys are labels and the values are objects of environment values for that label. Empty axes (`{}`) are ignored.

**`config.scenario`:**
```json
{
  "env_matrix": [
    {
      "admin": { "role": "admin", "auth_token": "sys-jwt-999" },
      "guest": { "role": "guest", "auth_token": "guest-jwt-111" }
    },
    {
      "card": { "method": "card", "expected_status": 200 },
      "crypto": { "method": "crypto", "expected_status": 202 }
    }
  ]
}
```

The scenario runs once for every way of picking one label from each axis, with that pick's values deep merged over its `env`. The example above gives 4 runs:

1.  **`admin_card`**: `role`="admin", `auth_token`="sys-jwt-999", `method`="card", `expected_status`=200
2.  **`admin_crypto`**: `role`="admin", `auth_token`="sys-jwt-999", `method`="crypto", `expected_status`=202
3.  **`guest_card`**: `role`="guest", `auth_token`="guest-jwt-111", `method`="card", `expected_status`=200
4.  **`guest_crypto`**: `role`="guest", `auth_token`="guest-jwt-111", `method`="crypto", `expected_status`=202

In the console report, each generated scenario is listed under the base scenario name, with its labels on the lines below it:

```text
 ✓ Scenario: checkout (1✓ in 12ms)
   Labels:
     admin
     card
```

In JUnit reports, the labels are appended to the suite name (`checkout_admin_card`), so every variant is its own test suite, and they're also recorded in a `labels` property.

!!! note
    The order the combinations run in is unspecified.

## Usage in Requests

Matrix values are ordinary `env` values: use `${env.variable}` in the request text or `env` in Lua.

**`01_checkout.sinq`:**
```sinq
POST ${env.BASE_URL}/checkout
Authorization: Bearer ${env.auth_token}
Content-Type: application/json

{
    "role": "${env.role}",
    "payment_method": "${env.method}"
}

$ASSERT{
    sinq.assert.code(env.expected_status, "Matrix status code mismatch for " .. env.role .. " using " .. env.method)
}
```

## Inheritance and Deep Merging

1.  **Matrix over `env`:** a combination's values are deep merged on top of the scenario's `env`, so they win on conflicts.
2.  **Axes add up:** if a parent and a child both define `env_matrix`, the child gets the parent's axes plus its own. The run count is the product of every axis's size.

!!! warning
    Nested matrices multiply quickly: three axes of 5 labels are 125 runs per scenario, times `--count`. The worker count stays at `-w`, but the total run time and load on the target grow accordingly.
