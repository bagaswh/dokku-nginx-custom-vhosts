# dokku-nginx-custom

## Configuration

The config might look like this:

```
vhosts:
  - existing: false
    server_name: app1.api.botika.online
    upstreams:
      - select: default
      - name: websocket-proxy
        servers:
          - addr: 127.0.0.1:
            flags: weight=1 max_fails=3 fail_timeout=30s

    locations:
      - uri: /app1/
        modifier: ~*
        body: |
          proxy_cache {{ .vars.proxy_cache_default }};
          proxy_pass http://{{ index $upstreams "default" }};

      - uri: /ws/
        body: |
          try_files /dev/null @{{ index $named_locations "websocket-proxy" }};
      
      - named: websocket-proxy
        body: |
          proxy_pass http://{{ index $upstreams "websocket-proxy" }}?name={{ index $variables "var1" }};` 

      - include: .dokku/locations.yaml

      maps:
        - variable: name
          string: $http
          lines: |
            hostnames;
            default 0;
            example.com 1;

      variables:
        - name: var1
          value: {{ index $map_variables "name" }}

    proxy_caches:
      - name: default-in-fs
        proxy_cache_path: {{ $proxy_fs_cache_path }}
        
      - name: default-in-mem
        proxy_cache_path: {{ $proxy_mem_cache_path }}

    fastcgi_caches: []

    in_server_block: ""
    in_http_block: ""

  - existing: true
    server_name: api.botika.online
    upstreams: []
    locations: []
```

`upstreams` config can either be a selector to the managed upstream in order to apply additional configuration to it, or a list of upstreams to create.