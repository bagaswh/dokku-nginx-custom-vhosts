# Dokku NGINX Path-Based VHOSTS Plugin

A Dokku plugin that allows multiple applications to be served under a single domain, distinguished by URL paths (e.g., `example.com/app1`, `example.com/app2`).

This plugin automates the NGINX configuration, including the creation of server blocks and upstream definitions for path-based routing. It centrally manages the configuration through a designated "default app" for each root domain.

## Features

-   **Path-Based Routing**: Host multiple Dokku apps on one domain.
-   **Automatic NGINX Configuration**: Automatically generates and manages NGINX configurations.
-   **Default App Concept**: Designate one app to handle requests to the root of the domain (`/`).
-   **Highly Configurable**: Control various NGINX properties like timeouts, log paths, and proxy settings via Dokku commands.
-   **Custom Templates**: Supports custom `nginx.conf.sigil` for full control over the NGINX configuration.

## Installation

```shell
sudo dokku plugin:install [https://github.com/szuryuu/nginx-custom-vhost.git](https://github.com/szuryuu/nginx-custom-vhost.git) nginx-custom-vhost
```

---

## Example Workflow

Here is a step-by-step example of how to set up two apps, `main-app` and `api-app`, to be served under `example.com`.

-   `main-app` will be accessible at `http://example.com/`
-   `api-app` will be accessible at `http://example.com/api`

#### 1. Set the Proxy Type for Each App

First, tell Dokku to use this plugin to manage web traffic for both apps.

```shell
dokku proxy:set main-app nginx-custom
dokku proxy:set api-app nginx-custom
```

#### 2. Configure Routing Properties

Now, configure the domain and paths for each application.

```shell
# Set the same root-domain for both apps
dokku nginx-custom:set main-app root-domain example.com
dokku nginx-custom:set api-app root-domain example.com

# Set the unique path for the secondary app
dokku nginx-custom:set api-app app-path api

# Designate the main app as the "default-app"
# Note: The default app handles requests to the root path ("/") and does not need an app-path.
dokku nginx-custom:set main-app default-app main-app
```

#### 3. Build the NGINX Configuration

Finally, build the NGINX configuration. This command must be run on the **default app**, as it controls the master configuration file for the domain.

```shell
dokku proxy:build-config main-app
```

Your applications should now be accessible at their configured paths.

---

## Commands

Here is a reference for the available commands.

#### Proxy Management
| Action | Command |
|---|---|
| **Set Proxy Type** | `dokku proxy:set <app_name> nginx-custom` |
| **Build Config** | `dokku proxy:build-config <default_app_name>` |

#### Routing Configuration
| Property | Command |
|---|---|
| **`app-path`** | `dokku nginx-custom:set <app_name> app-path <path>` |
| **`root-domain`** | `dokku nginx-custom:set <app_name> root-domain <domain>` |
| **`default-app`** | `dokku nginx-custom:set <app_name> default-app <app_name>` |
| **Unset a property** | `dokku nginx-custom:set <app_name> default-app` |

#### Troubleshooting & Inspection
| Action | Command | Notes |
|---|---|---|
| **Check a Property** | `dokku nginx-custom:get <app_name> default-app` | If the property is set, it returns the value. Otherwise, it returns empty. |
| **Validate Config** | `dokku nginx-custom:validate-config` | An empty return means the configuration is valid. |
| **View App Report** | `dokku nginx-custom:report <app_name>` | Shows a detailed report of all NGINX properties for the app. |
| **Show NGINX Config** | `dokku nginx-custom:show-config <app_name>` | Only works for the `default-app`, as it holds the master config file. |
| **View Access Logs**| `dokku nginx-custom:access-logs <app_name> -t` | |
| **View Error Logs** | `dokku nginx-custom:error-logs <app_name> -t` | |

---

## Configuration Properties

You can customize NGINX behavior using the `nginx-custom:set` command. Properties can be set per-app or globally using the `--global` flag.

**Example:**
```shell
# Set the max client body size for a single app
dokku nginx-custom:set my-app client-max-body-size 10m

# Set the proxy read timeout globally for all apps using this plugin
dokku nginx-custom:set --global proxy-read-timeout 120s
```

For a full list of configurable properties, see the `src/nginx-property/nginx_vhosts.go` file in this repository.
