# Cloud Foundry Route Lookup Plugin

This is a Cloud Foundry CLI plugin to quickly identify applications, a given route is pointing to.
Note this will only show applications in organizations and spaces, that the logged-in user has permissions to view.
The plugin also supports targeting to the organization and space of the applications, a given route is pointing to.

## Installation

1. Download the appropriate binary from [the Releases page](https://github.com/cloudfoundry/cf-lookup-route/releases).
2. Run

    ```sh
    cf install-plugin -r CF-Community PATH_TO_ROUTE_LOOKUP_BIN
    ```

## Usage

```
$ cf lookup-route <my.example.com>
Bound to:
Organization: <org> (<org_guid>)
Space       : <space> (<space_guid>)
App         : <app1> (<app_guid_1>)
App         : <app2> (<app_guid_2>)

#use -t to target the org/space containing the route
$ cf lookup-route -t <my.example.com>

Bound to:
Organization: <org> (<org_guid>)
Space       : <space> (<space_guid>)
App         : <app> (<app_guid>)

Targeting an app's organization and space...
<cf target command output>
Targeting an app's organization and space successful.

$ cf lookup-route <unknown.example.com>
Error retrieving apps: Route <unknown.example.com> not found.
```