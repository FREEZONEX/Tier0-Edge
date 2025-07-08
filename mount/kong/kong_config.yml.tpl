_transform: false
_format_version: '3.0'
consumers:
- id: 59d1ef15-24a5-4373-b957-e8192c15ff6e
  created_at: 1734329209
  updated_at: 1749260359
  tags: []
  username: open-api
  custom_id: ~
parameters:
- key: cluster_id
  created_at: 1742432166
  value: ce8f9346-2b44-46af-af55-ab141518e1bb
services:
- tls_verify: ~
  tls_verify_depth: ~
  id: 1f8b86e3-6352-47a3-ac19-01537a11bec9
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1733117004
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: wenhao
  retries: 5
  path: ~
  name: wenhao
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 228308d0-521c-45f8-a97d-fbe6966efa3c
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729740320
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: grafana
  retries: 5
  path: ~
  name: grafana
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 24a5cd06-6728-48ff-a7c8-1847c836bd59
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729852876
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: gitea
  retries: 5
  path: ~
  name: gitea
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 29bf0a09-9e84-449e-b660-d0c9bf24cab3
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729740396
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: hasura
  retries: 5
  path: ~
  name: hasura
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 2cd97a86-3853-4001-9f0d-7769dc40d508
  connect_timeout: 60000
  protocol: https
  read_timeout: 60000
  created_at: 1729740912
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 9443
  host: portainer
  retries: 5
  path: ~
  name: portainer
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 2df2f7c3-792a-4a30-b30c-6011e2f3f916
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729736772
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 1337
  host: konga
  retries: 5
  path: /
  name: konga
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 33c02e25-db50-4332-a044-094fce5f50b0
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1749280964
  updated_at: 1749280964
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3001
  host: plugin
  retries: 5
  path: ~
  name: plugin-frontend
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 4007e6c1-6ccc-4747-9b96-7bb3f5f78b32
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1734330146
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /open-api/supos/
  name: backend-open-api
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 42684a67-ac8f-48d6-ae2b-62e1cd26f9d8
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1742976133
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /service-api/supos/proxy/event/flows
  name: event-flow-proxy
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 43a4afb4-cabf-4fd8-a3f4-d8b31408e6dc
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729740779
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 5601
  host: kibana
  retries: 5
  path: /elastic/home/
  name: elastic
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 4d210886-a694-416d-847d-95fc597f5921
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1733290847
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /service-api/supos/
  name: backend-service-api
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 5e8458a8-7323-4a25-9efa-4d56dbf5fa5b
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1732610867
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8070
  host: gateway
  retries: 5
  path: /
  name: gateway
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 5f70fd49-e3d7-4ba8-b152-62eca6ec4455
  connect_timeout: 300000
  protocol: http
  read_timeout: 300000
  created_at: 1729740054
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /inter-api/supos/
  name: backend
  write_timeout: 300000
- tls_verify: ~
  tls_verify_depth: ~
  id: 61c91a4e-3e0c-4253-abad-de0a0ff72369
  connect_timeout: 300000
  protocol: http
  read_timeout: 300000
  created_at: 1736404121
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /swagger-ui/
  name: open-api-docs
  write_timeout: 300000
- tls_verify: ~
  tls_verify_depth: ~
  id: 647747b1-8efe-45b6-b37f-430f5e5139d6
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1731396402
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 9001
  host: minio
  retries: 5
  path: /
  name: minio
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 6a3bcaba-7ba3-4c3b-b5c2-1a8ecbc571ac
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1742536198
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 1889
  host: eventflow
  retries: 5
  path: ~
  name: EventFlow
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 75a7373b-4dda-4a49-b1f8-de6ebee4d4c8
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1741670682
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /files
  name: backend-files
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 776a9d80-b936-4e00-b892-3861b19dd954
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1742538082
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: TDengineUI
  retries: 5
  path: ~
  name: TDengineUI
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 89c6a77c-e3e6-4149-a0e7-6ace7fd0413b
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729740676
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 18083
  host: emqx
  retries: 5
  path: ~
  name: emqx
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729738059
  updated_at: 1749260359
  tags:
  - root:frontend
  - Home:1
  - SourceFlow:2
  - Namespace:3
  - EventFlow:4
  - CollectionGatewayManagement:5
  - menu.tag.devtools:6
  - menu.tag.uns:1
  - menu.tag.appspace:7
  - menu.tag.system:8
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: frontend
  retries: 5
  path: ~
  name: frontend
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: abdb1947-dbbf-4a85-ac55-50969ac0ed85
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1733732987
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 1881
  host: fuxa
  retries: 5
  path: ~
  name: fuxa
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: b096bcf5-2984-4acc-9bd5-a570a7653fcd
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1730685511
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: backend
  retries: 5
  path: /service-api/supos/proxy/flows
  name: node-red-proxy
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: b2a70de2-d5db-4755-b8ba-b205d8fbb680
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729740513
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: keycloak
  retries: 5
  path: /
  name: keycloak
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: bba8a174-4679-49df-8bf9-ae9285f1e77e
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1729739239
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 1880
  host: nodered
  retries: 5
  path: ~
  name: nodered
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: c53d8d7a-962e-4561-accc-ddf6447336df
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1741747300
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 10824
  host: chat2db
  retries: 5
  path: /
  name: chat2db-backend
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: cc3e8a53-82e1-4f6e-bde6-75207ca2f6d3
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1731460027
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 9000
  host: minio-inter
  retries: 5
  path: ~
  name: minio-inter
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: d9ac42d8-9d41-41e3-a80f-4b64cc215bcf
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1741747189
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3001
  host: platform
  retries: 5
  path: /
  name: chat2db
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: df4453f0-063a-4346-a074-3c2f9a388ca7
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1744972980
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 1880
  host: nodered
  retries: 5
  path: /nodered-api/
  name: nodered-backend
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: e3e88607-311a-4c23-a9c7-bb879efc463e
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1732105495
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: keycloak
  retries: 5
  path: /realms/supos/protocol/openid-connect/auth
  name: login
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: f145ba5c-e9aa-48a6-8a10-9ee476010f7f
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1730264819
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 8080
  host: apm
  retries: 5
  path: ~
  name: apm
  write_timeout: 60000
- tls_verify: ~
  tls_verify_depth: ~
  id: f8fd7fd2-d8f6-47d7-9c54-aa51a24a68ad
  connect_timeout: 600000
  protocol: http
  read_timeout: 600000
  created_at: 1729748350
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 4000
  host: copilotkit
  retries: 5
  path: /copilotkit
  name: GenerativeUI
  write_timeout: 600000
- tls_verify: ~
  tls_verify_depth: ~
  id: fbc56015-44d7-42ba-ac9f-abe725bc2478
  connect_timeout: 60000
  protocol: http
  read_timeout: 60000
  created_at: 1742543141
  updated_at: 1749260359
  tags: []
  ca_certificates: ~
  client_certificate: ~
  enabled: true
  port: 3000
  host: mcpclient
  retries: 5
  path: ~
  name: mcpclient
  write_timeout: 60000
routes:
- sources: ~
  destinations: ~
  id: 050c90d4-b22d-4d44-a159-0d35c949a111
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730253324
  updated_at: 1749260359
  service: 43a4afb4-cabf-4fd8-a3f4-d8b31408e6dc
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /elastic/home/app/discover#/?_g=(filters:!(),refreshInterval:(pause:!t,value:60000),time:(from:now-15m,to:now))&_a=(columns:!(container.name,message),dataSource:(type:dataView),filters:!(),grid:(columns:(container.name:(width:301))),index:'31d5c920-8e8a-11ef-80e3-d78ebb901f76',interval:auto,query:(language:kuery,query:''),sort:!(!('@timestamp',desc)))
  strip_path: true
  name: Logs
  protocols:
  - http
  - https
  tags:
  - ${ENABLE_ELK_MENU}
  - description:menu.desc.logs
  - sort:7
  - parentName:menu.tag.system
  snis: ~
- sources: ~
  destinations: ~
  id: 0e566d44-48f2-442e-a7e9-b288926f3e88
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1742538180
  updated_at: 1749260359
  service: 776a9d80-b936-4e00-b892-3861b19dd954
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /
  strip_path: true
  name: TDengineUI
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 10c662d8-8304-439b-85a0-398ae09d09e8
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1730270842
  updated_at: 1749260359
  service: 228308d0-521c-45f8-a97d-fbe6966efa3c
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /grafana/home/
  strip_path: true
  name: grafana-inter
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 15023439-25a9-481d-90a5-894d4d87d3bf
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730254514
  updated_at: 1749260359
  service: 24a5cd06-6728-48ff-a7c8-1847c836bd59
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /gitea/home/user/login?redirect_to=/gitea/home/
  strip_path: true
  name: CICD
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.cicd
  - sort:1
  - parentName:menu.tag.devtools
  - menu
  snis: ~
- sources: ~
  destinations: ~
  id: 1dfa8b71-07e8-41a5-a07d-536fa0e295a2
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731311256
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /app-display
  strip_path: true
  name: GenApps
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.genApps
  - sort:2
  - parentName:menu.tag.appspace
  snis: ~
- sources: ~
  destinations: ~
  id: 1fe717e7-804d-473b-b12b-7f5f68b5ec48
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1734328715
  updated_at: 1749260359
  service: 29bf0a09-9e84-449e-b660-d0c9bf24cab3
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /open-api/graphql/
  strip_path: true
  name: open-hasura-graphql
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 209f6169-707c-49f1-86cc-602bc4674b2e
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730265781
  updated_at: 1749260359
  service: f145ba5c-e9aa-48a6-8a10-9ee476010f7f
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /apps/freezonex-aps/
  strip_path: true
  name: apm-backend-iner
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 215cdb87-6e13-4e2e-804f-9ea1ac2ff417
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729740717
  updated_at: 1749260359
  service: 89c6a77c-e3e6-4149-a0e7-6ace7fd0413b
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /emqx/home/
  strip_path: true
  name: MqttBroker
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.mqttBroker
  - parentName:menu.tag.system
  - sort:3
  snis: ~
- sources: ~
  destinations: ~
  id: 29027b71-49ce-41d1-96fc-14bcb3a2da00
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729748378
  updated_at: 1749260359
  service: f8fd7fd2-d8f6-47d7-9c54-aa51a24a68ad
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /copilotkit
  strip_path: true
  name: copilotkit
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 366d2cfa-8a0e-4a6b-943e-3b5ecd424f73
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1748310135
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /plugin-management
  strip_path: true
  name: PluginManagement
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.PluginManagement
  - parentName:menu.tag.system
  - sort:14
  snis: ~
- sources: ~
  destinations: ~
  id: 3794799e-0c23-4065-a88d-7a08c46fbaf4
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1729740083
  updated_at: 1749260359
  service: 5f70fd49-e3d7-4ba8-b152-62eca6ec4455
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /inter-api/supos/
  strip_path: true
  name: backend
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 3f8f8bf7-4d53-4a11-bc6d-d017beda8695
  methods:
  - GET
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730685544
  updated_at: 1749260359
  service: b096bcf5-2984-4acc-9bd5-a570a7653fcd
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /nodered/home/flows
  strip_path: true
  name: node-red-flows
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: 45a9fec1-6eaa-49c5-ae00-7aa180f1efde
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1742536226
  updated_at: 1749260359
  service: 6a3bcaba-7ba3-4c3b-b5c2-1a8ecbc571ac
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /eventflow/home/
  strip_path: true
  name: EventFlowBackend
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: 4d89ed56-90bf-490d-ac8c-30def8be2e2c
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731311044
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /uns
  strip_path: true
  name: Namespace
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.dataModeling
  - homeParentName:menu.tag.uns
  - homeIconUrl:homeNamespace
  - sort:2
  snis: ~
- sources: ~
  destinations: ~
  id: 4e5336db-053c-4c9f-a773-bb83a2557ffb
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1741747272
  updated_at: 1749260359
  service: d9ac42d8-9d41-41e3-a80f-4b64cc215bcf
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /chat2db/home/connections
  strip_path: true
  name: DBConnect
  protocols:
  - http
  - https
  tags:
  - menu
  - parentName:menu.tag.devtools
  - description:menu.desc.dbconnect
  - sort:2
  snis: ~
- sources: ~
  destinations: ~
  id: 4f4b9999-c921-4ca8-9eef-63db46e6b2b5
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729740461
  updated_at: 1749260359
  service: 29bf0a09-9e84-449e-b660-d0c9bf24cab3
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /hasura/home/
  strip_path: true
  name: graphQL
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.graphQL
  snis: ~
- sources: ~
  destinations: ~
  id: 5a0cdda1-0ac2-4255-a4bc-a11a8b4a00d5
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1733290917
  updated_at: 1749260359
  service: 4d210886-a694-416d-847d-95fc597f5921
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /service-api/supos/
  strip_path: true
  name: backend-service-api
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 5a9793a9-5085-4830-bba7-a5c053a055a1
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1744972998
  updated_at: 1749260359
  service: df4453f0-063a-4346-a074-3c2f9a388ca7
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /nodered-api/
  strip_path: true
  name: nodered-backend
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 6239b86d-4ef1-48d9-b512-7b05306ab705
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1742543248
  updated_at: 1749260359
  service: fbc56015-44d7-42ba-ac9f-abe725bc2478
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /mcpclient/home
  strip_path: false
  name: McpClient
  protocols:
  - http
  - https
  tags:
  - ${ENABLE_MCP}
  - parentName:menu.tag.appspace
  - description:menu.desc.mcpclient
  - sort:3
  snis: ~
- sources: ~
  destinations: ~
  id: 670008e2-5811-4d9d-b925-5429d16caa8f
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1741671187
  updated_at: 1749260359
  service: 75a7373b-4dda-4a49-b1f8-de6ebee4d4c8
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /files
  strip_path: true
  name: backend-files
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 6856a277-bcae-43a3-992c-81fc5c605baf
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1734328933
  updated_at: 1749260359
  service: 29bf0a09-9e84-449e-b660-d0c9bf24cab3
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /open-api/restapi/
  strip_path: true
  name: open-hasura-restapi
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 6ce6b319-0e38-4bf4-ba1d-4d043a926ba7
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1732611195
  updated_at: 1749260359
  service: 5e8458a8-7323-4a25-9efa-4d56dbf5fa5b
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /gateway
  strip_path: true
  name: gateway
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 6f2a8d9e-d76f-46b9-b2d4-27286651433d
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730264872
  updated_at: 1749260359
  service: f145ba5c-e9aa-48a6-8a10-9ee476010f7f
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /apps/freezonex-aps/apsfrontend/
  strip_path: false
  name: apm-frontend-inter
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 78e5de54-af01-4eff-bb5d-bfbde4f35835
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1741747323
  updated_at: 1749260359
  service: c53d8d7a-962e-4561-accc-ddf6447336df
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /chat2db/
  strip_path: true
  name: chat2dbapi
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 79d5e57d-340c-4d18-93ee-a6a8f4a0f212
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729736896
  updated_at: 1749260359
  service: 2df2f7c3-792a-4a30-b30c-6011e2f3f916
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /konga/home/
  strip_path: true
  name: RoutingManagement
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.konga
  - sort:1
  - parentName:menu.tag.system
  - menu
  snis: ~
- sources: ~
  destinations: ~
  id: 8154564f-57c9-4de4-a605-a204c46bf15a
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729740838
  updated_at: 1749260359
  service: 43a4afb4-cabf-4fd8-a3f4-d8b31408e6dc
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /elastic/home/
  strip_path: true
  name: elastic-inter
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: 833d37e3-05bb-4a1d-992c-0a0e5c19b0dd
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1730265586
  updated_at: 1749260359
  service: f145ba5c-e9aa-48a6-8a10-9ee476010f7f
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /apsfrontend/dashboard
  strip_path: false
  name: apm
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.apm
  snis: ~
- sources: ~
  destinations: ~
  id: 84235d0d-bc92-4a86-9413-a66807af7f90
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1749280989
  updated_at: 1749280989
  service: 33c02e25-db50-4332-a044-094fce5f50b0
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /plugin/
  strip_path: true
  name: plugin-frontend
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: 8613d94b-0fd7-4b81-8f9d-ac5df267b7e3
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1733209593
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /streams
  strip_path: true
  name: StreamProcessing
  protocols:
  - http
  - https
  tags:
  - parentName:menu.tag.connections
  snis: ~
- sources: ~
  destinations: ~
  id: 86fbf4ad-d446-47e3-a9ba-00ead9113c17
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1733733028
  updated_at: 1749260359
  service: abdb1947-dbbf-4a85-ac55-50969ac0ed85
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /fuxa/home/
  strip_path: true
  name: LowCodeTool
  protocols:
  - http
  - https
  tags:
  - parentName:menu.tag.connections
  snis: ~
- sources: ~
  destinations: ~
  id: 95100aa4-a76b-481c-8485-56eb1f71a529
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1733117175
  updated_at: 1749260359
  service: 1f8b86e3-6352-47a3-ac19-01537a11bec9
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /
  strip_path: true
  name: Ollama
  protocols:
  - http
  - https
  tags:
  - menuPort:13020
  - menuProtocol:http
  - menuHost:${ENTRANCE_DOMAIN}
  - description:The llm webUI which is  easy to use
  snis: ~
- sources: ~
  destinations: ~
  id: 9df937e7-2ffb-49f4-b60b-4bb5b551419a
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1734330177
  updated_at: 1749260359
  service: 4007e6c1-6ccc-4747-9b96-7bb3f5f78b32
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /open-api/supos/
  strip_path: true
  name: open-backend-api
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: a13bb597-9740-4dde-929e-d140c286d869
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731311377
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /collection-flow
  strip_path: true
  name: SourceFlow
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.nodered.flow
  - sort:1
  - homeParentName:menu.tag.uns
  - homeIconUrl:homeSourceFlow
  snis: ~
- sources: ~
  destinations: ~
  id: a1f72e00-26fa-47a6-997c-a3c6c6aef9e7
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1736404187
  updated_at: 1749260359
  service: 61c91a4e-3e0c-4253-abad-de0a0ff72369
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /swagger-ui
  strip_path: true
  name: open-api-docs
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: a2060aa0-88e5-4247-9635-f93438bbdd84
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731311359
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /app-space
  strip_path: true
  name: GenerativeUI
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.generativeUI
  - parentName:menu.tag.appspace
  - sort:2
  snis: ~
- sources: ~
  destinations: ~
  id: a5040934-75dd-40c6-94ea-9497ab2b0579
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1741573643
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /workflow
  strip_path: true
  name: workflow
  protocols:
  - http
  - https
  tags:
  - parentName:menu.tag.settings
  snis: ~
- sources: ~
  destinations: ~
  id: a6d04fe9-a464-493c-8f3a-4750fdd93a32
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731396438
  updated_at: 1749260359
  service: 647747b1-8efe-45b6-b37f-430f5e5139d6
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /minio/home/
  strip_path: true
  name: objectStorageServer
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.objectStorageServer
  - sort:200
  snis: ~
- sources: ~
  destinations: ~
  id: aa02bbb9-6459-43bd-9b65-91d89c8854dd
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1730270517
  updated_at: 1749260359
  service: 228308d0-521c-45f8-a97d-fbe6966efa3c
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /grafana/home/dashboards/
  strip_path: true
  name: grafana
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.grafana
  snis: ~
- sources: ~
  destinations: ~
  id: b610973a-764e-4cef-910e-0794f334e4bd
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731473911
  updated_at: 1749260359
  service: b2a70de2-d5db-4755-b8ba-b205d8fbb680
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /keycloak/home/auth/
  strip_path: true
  name: keycloak-auth
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: b8262364-32bf-4422-9d6c-04b97bc8c3a7
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1742968905
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /EventFlow
  strip_path: true
  name: EventFlow
  protocols:
  - http
  - https
  tags:
  - menu
  - homeParentName:menu.tag.uns
  - description:menu.desc.eventflow
  - homeIconUrl:homeEventFlow
  - sort:3
  snis: ~
- sources: ~
  destinations: ~
  id: ba7b2df9-b0d8-4b6b-844d-43f935f3181f
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1732108769
  updated_at: 1749260359
  service: e3e88607-311a-4c23-a9c7-bb879efc463e
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /supos-login
  strip_path: true
  name: login
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: c2dececa-99f4-45e1-9859-01e88352bd58
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729738250
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /
  strip_path: true
  name: frontend
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: c90b4b7a-8a51-4f40-b4e2-6c0a40be1b15
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731635333
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /home
  strip_path: true
  name: Home
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.home
  - sort:1
  snis: ~
- sources: ~
  destinations: ~
  id: d0cea78f-1e0d-4b90-98ea-980a455bf5f5
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729740574
  updated_at: 1749260359
  service: b2a70de2-d5db-4755-b8ba-b205d8fbb680
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /keycloak/home/
  strip_path: true
  name: Authentication
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.keycloak
  - menu
  - sort:2
  - parentName:menu.tag.system
  snis: ~
- sources: ~
  destinations: ~
  id: d2a81d6f-8b2a-4b28-8929-3c51ccd16021
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1730770040
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /dashboards
  strip_path: true
  name: Dashboards
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.dashboards
  - parentName:menu.tag.system
  - sort:5
  snis: ~
- sources: ~
  destinations: ~
  id: dbb92267-e886-4ee8-b758-a9f9e9af1998
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1731460050
  updated_at: 1749260359
  service: cc3e8a53-82e1-4f6e-bde6-75207ca2f6d3
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /minio/inter/
  strip_path: true
  name: minio-inter
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: e3459f2d-fcb2-412e-87fc-b098d8906b7e
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729852903
  updated_at: 1749260359
  service: 24a5cd06-6728-48ff-a7c8-1847c836bd59
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /gitea/home/
  strip_path: true
  name: gitea-inter
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: e3ff5178-c156-47e6-90f4-ec2a45b05953
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1741747250
  updated_at: 1749260359
  service: d9ac42d8-9d41-41e3-a80f-4b64cc215bcf
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /chat2db/home/
  strip_path: true
  name: chat2db-inter
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: e8e7fe7d-16ba-415a-8d19-e2c41b76b365
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1742976255
  updated_at: 1749260359
  service: 42684a67-ac8f-48d6-ae2b-62e1cd26f9d8
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /eventflow/home/flows
  strip_path: true
  name: event-node-flows
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: ea381a8a-a76a-4043-9e05-d5e5c9bf3492
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1733118225
  updated_at: 1749260359
  service: 5e8458a8-7323-4a25-9efa-4d56dbf5fa5b
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /test
  strip_path: true
  name: 测试中文
  protocols:
  - http
  - https
  tags: ~
  snis: ~
- sources: ~
  destinations: ~
  id: ed108474-cc43-4b66-b92a-5de38f061c6d
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: true
  https_redirect_status_code: 426
  created_at: 1741747220
  updated_at: 1749260359
  service: d9ac42d8-9d41-41e3-a80f-4b64cc215bcf
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /chat2db/home/workspace
  strip_path: true
  name: SQLEditor
  protocols:
  - http
  - https
  tags:
  - menu
  - parentName:menu.tag.devtools
  - description:menu.desc.sqledit
  - sort:4
  snis: ~
- sources: ~
  destinations: ~
  id: f0a59836-7eea-45b3-b188-51c45c68f305
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1734056913
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /advanced-use
  strip_path: true
  name: AdvancedUse
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.advanceUse
  - sort:9
  - parentName:menu.tag.system
  snis: ~
- sources: ~
  destinations: ~
  id: f610002e-8e5c-424a-9fdc-e86d754b3142
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1732696995
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /account-management
  strip_path: true
  name: UserManagement
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.account
  - parentName:menu.tag.system
  - sort:4
  snis: ~
- sources: ~
  destinations: ~
  id: f8bccdbf-46fd-4dd7-aafe-d1a8e13c3450
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729739343
  updated_at: 1749260359
  service: bba8a174-4679-49df-8bf9-ae9285f1e77e
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /nodered/home/
  strip_path: true
  name: NodeRed
  protocols:
  - http
  - https
  tags: []
  snis: ~
- sources: ~
  destinations: ~
  id: fb690c3a-e09e-4fcf-aad1-5d357c1938ec
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1729740933
  updated_at: 1749260359
  service: 2cd97a86-3853-4001-9f0d-7769dc40d508
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /portainer/home/
  strip_path: true
  name: ContainerManagement
  protocols:
  - http
  - https
  tags:
  - description:menu.desc.dockerMgmt
  - ${ENABLE_PORTAINER}
  - sort:6
  - parentName:menu.tag.system
  snis: ~
- sources: ~
  destinations: ~
  id: fb8efe62-0030-4540-a521-a3b4215b4f59
  methods: ~
  regex_priority: 0
  hosts: ~
  preserve_host: false
  https_redirect_status_code: 426
  created_at: 1733822464
  updated_at: 1749260359
  service: 8e081976-8223-4494-9b4c-0aa5a441bdd5
  request_buffering: true
  path_handling: v1
  response_buffering: true
  headers: ~
  paths:
  - /aboutus
  strip_path: true
  name: AboutUs
  protocols:
  - http
  - https
  tags:
  - menu
  - description:menu.desc.aboutus
  - parentName:menu.tag.system
  - sort:8
  snis: ~
plugins:
- consumer: ~
  created_at: 1733799127
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: ~
  id: 1845ee75-d704-40e1-a8b0-aa2baaf9d71b
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    forbidden_url: /403
    enable_resource_check: true
    enable_deny_check: true
    login_url: ${BASE_URL}/keycloak/home/auth/realms/supos/protocol/openid-connect/auth?client_id=supos&redirect_uri=${BASE_URL}/inter-api/supos/auth/token&response_type=code&scope=openid
    whitelist_paths:
    - ^/inter-api/supos/auth.*$
    - ^/inter-api/supos/systemConfig.*$
    - ^/inter-api/supos/theme/getConfig.*$
    - ^/$
    - ^/assets.*$
    - ^/locale.*$
    - ^/logo.*$
    - ^/gitea.*git.*$
    - ^/supos-login.*$
    - ^/403$
    - ^/open-api/.*$
    - ^/keycloak.*$
    - ^/nodered.*$
    - ^/files.*$
    - ^/freeLogin.*$
    - ^/inter-api/supos/dev/logs.*$
  name: supos-auth-checker
- consumer: ~
  created_at: 1734330234
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: 9df937e7-2ffb-49f4-b60b-4bb5b551419a
  id: 2285421f-56e3-4510-be12-69fa1040d810
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    run_on_preflight: true
    key_names:
    - apikey
    hide_credentials: false
    anonymous: ~
    realm: ~
    key_in_query: true
    key_in_body: false
    key_in_header: true
  name: key-auth
- consumer: ~
  created_at: 1734073535
  updated_at: 1749260359
  service: e3e88607-311a-4c23-a9c7-bb879efc463e
  tags: ~
  instance_name: ~
  route: ~
  id: 46bda5cf-63ea-401f-9f06-b9e024aa5597
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    append:
      querystring:
      - client_id:supos
      - response_type:code
      - scope:openid
      - redirect_uri:${BASE_URL}/inter-api/supos/auth/token
      body: []
      headers: []
    replace:
      querystring: []
      uri: ~
      headers: []
      body: []
    rename:
      querystring: []
      body: []
      headers: []
    http_method: ~
    add:
      querystring: []
      body: []
      headers: []
    remove:
      querystring: []
      body: []
      headers: []
  name: request-transformer
- consumer: ~
  created_at: 1734329742
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: 1fe717e7-804d-473b-b12b-7f5f68b5ec48
  id: 4e339a48-e193-4bc7-99ba-94f2e1e18a8f
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    run_on_preflight: true
    key_names:
    - apikey
    hide_credentials: false
    anonymous: ~
    realm: ~
    key_in_query: true
    key_in_body: false
    key_in_header: true
  name: key-auth
- consumer: ~
  created_at: 1734073759
  updated_at: 1749260359
  service: e3e88607-311a-4c23-a9c7-bb879efc463e
  tags: ~
  instance_name: ~
  route: ~
  id: 73285cac-cf4e-4368-bf8f-d3285f9686d4
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    append:
      json: []
      json_types: []
      headers:
      - X-Frame-Options:DENY
      - Content-Security-Policy:frame-ancestors 'none'
    replace:
      json: []
      json_types: []
      headers: []
    rename:
      json: []
      headers: []
    add:
      json: []
      json_types: []
      headers: []
    remove:
      json: []
      headers:
      - x-frame-options
      - Content-Security-Policy
  name: response-transformer
- consumer: ~
  created_at: 1734329166
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: 6856a277-bcae-43a3-992c-81fc5c605baf
  id: 9e4c8423-76d1-498d-8883-b9b18bdbf161
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    run_on_preflight: true
    key_names:
    - apikey
    hide_credentials: false
    anonymous: ~
    realm: ~
    key_in_query: true
    key_in_body: false
    key_in_header: true
  name: key-auth
- consumer: ~
  created_at: 1731400906
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: a6d04fe9-a464-493c-8f3a-4750fdd93a32
  id: b5722a76-60b9-483d-90ac-e5de173264e2
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    append:
      json: []
      json_types: []
      headers: []
    replace:
      json: []
      json_types: []
      headers: []
    rename:
      json: []
      headers: []
    add:
      json: []
      json_types: []
      headers:
      - X-Frame-Options:SAMEORIGIN
    remove:
      json: []
      headers:
      - X-Frame-Options
  name: response-transformer
- consumer: ~
  created_at: 1734014838
  updated_at: 1749260359
  service: ~
  tags: ~
  instance_name: ~
  route: ba7b2df9-b0d8-4b6b-844d-43f935f3181f
  id: f45e7fd0-74e5-4b36-b265-5df962eb1b58
  enabled: true
  protocols:
  - grpc
  - grpcs
  - http
  - https
  config:
    home_url: /?isLogin=true
  name: supos-url-transformer
upstreams:
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 00f62da1-73b3-4308-8ae5-856268388aa9
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1730264799
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: apm
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 09fa87e7-1cf1-4fc3-9ad6-6861572a8ef4
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1731459961
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: minio-inter
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 14bdf793-231d-439e-8d67-3e47a3e7da05
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1741754738
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: platform
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 14ee49e7-f9bf-4234-a48f-7b7df7dda0ea
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739799
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: keycloak
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 220c1252-a48b-4e7e-af42-b134f316ed16
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739256
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: nodered
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 2a0fa8a9-98a4-4456-9d2c-faba35b54882
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729737217
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: konga
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 2e9455f1-241d-4a1e-840e-fea66dd9aa09
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739617
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: grafana
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 3c840281-aeb3-4969-8ea4-cc12d45423a1
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749280889
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1749280889
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: plugin
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 3f51d2ee-7609-4f71-b6ec-8ded27b417a2
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739903
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: portainer
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 420478e2-bdc8-49ec-ba0e-cc4cfd41afc8
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739883
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: kibana
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 47b5a73e-51c6-4ace-b507-40458125c0a6
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729852910
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: gitea
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: 747a43cc-42c4-457a-abac-518c2fe537b3
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739930
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: copilotkit
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: ab6fb6a2-97b9-45e7-b508-46df2df5203e
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739734
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: hasura
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: c0645f32-b63d-49ba-a723-5cbef9014c99
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729737694
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: frontend
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: c6844f11-b711-4f5f-a2d4-4516995790c5
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1731396351
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: minio
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: e6729ab9-c894-4963-bbdc-dfae17c88096
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739594
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: backend
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: fa9fc31a-7d71-43bd-a453-c21704b71ac6
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1729739828
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: emqx
  hash_on_query_arg: ~
- hash_on_uri_capture: ~
  use_srv_name: false
  id: fc85cfef-6e90-4592-a022-730f5d84ed99
  hash_on_cookie: ~
  hash_on_cookie_path: /
  client_certificate: ~
  host_header: ~
  hash_fallback: none
  updated_at: 1749260359
  algorithm: round-robin
  hash_fallback_uri_capture: ~
  tags: ~
  healthchecks:
    passive:
      healthy:
        successes: 0
        http_statuses:
        - 200
        - 201
        - 202
        - 203
        - 204
        - 205
        - 206
        - 207
        - 208
        - 226
        - 300
        - 301
        - 302
        - 303
        - 304
        - 305
        - 306
        - 307
        - 308
      type: http
      unhealthy:
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 500
        - 503
    threshold: 0
    active:
      type: http
      headers: ~
      healthy:
        successes: 0
        interval: 0
        http_statuses:
        - 200
        - 302
      unhealthy:
        interval: 0
        tcp_failures: 0
        timeouts: 0
        http_failures: 0
        http_statuses:
        - 429
        - 404
        - 500
        - 501
        - 502
        - 503
        - 504
        - 505
      timeout: 1
      http_path: /
      https_sni: ~
      https_verify_certificate: true
      concurrency: 10
  hash_fallback_query_arg: ~
  created_at: 1733536118
  slots: 1000
  hash_on: none
  hash_fallback_header: ~
  hash_on_header: ~
  name: fuxa
  hash_on_query_arg: ~
targets:
- created_at: 1730264807.829
  updated_at: 1749260359.736
  tags: ~
  upstream: 00f62da1-73b3-4308-8ae5-856268388aa9
  id: 0be879dd-7ba3-42e3-96a8-5be0c8dcadfe
  weight: 100
  target: apm:8080
- created_at: 1729739629.259
  updated_at: 1749260359.738
  tags: ~
  upstream: 2e9455f1-241d-4a1e-840e-fea66dd9aa09
  id: 0c66a055-3e1d-4e77-aedd-6de30939a5a2
  weight: 100
  target: grafana:3000
- created_at: 1729852918.487
  updated_at: 1749260359.733
  tags: ~
  upstream: 47b5a73e-51c6-4ace-b507-40458125c0a6
  id: 29a2c68c-7501-49b1-aef1-56e1be3c2d74
  weight: 100
  target: gitea:3000
- created_at: 1733536131.451
  updated_at: 1749260359.731
  tags: ~
  upstream: fc85cfef-6e90-4592-a022-730f5d84ed99
  id: 2cc80d27-504d-4c1a-afaf-646f6e432fd6
  weight: 100
  target: fuxa:1881
- created_at: 1731397614.344
  updated_at: 1749260359.743
  tags: ~
  upstream: c6844f11-b711-4f5f-a2d4-4516995790c5
  id: 2f312fcf-baa6-4de0-84aa-0779713ae279
  weight: 100
  target: minio:9001
- created_at: 1729737239.883
  updated_at: 1749260359.739
  tags: ~
  upstream: 2a0fa8a9-98a4-4456-9d2c-faba35b54882
  id: 38ae0eeb-fa36-40a1-8c72-aaa43e7b9ceb
  weight: 100
  target: konga:1337
- created_at: 1729739844.316
  updated_at: 1749260359.734
  tags: ~
  upstream: fa9fc31a-7d71-43bd-a453-c21704b71ac6
  id: 3ec4626c-673a-4cc1-86fa-10f3b1bdc537
  weight: 100
  target: emqx:18083
- created_at: 1729739274.174
  updated_at: 1749260359.745
  tags: ~
  upstream: 220c1252-a48b-4e7e-af42-b134f316ed16
  id: 5a923a6d-0661-43ad-a668-e89f4980e5a6
  weight: 100
  target: nodered:1880
- created_at: 1729739915.095
  updated_at: 1749260359.73
  tags: ~
  upstream: 3f51d2ee-7609-4f71-b6ec-8ded27b417a2
  id: 78410225-c503-4d2c-885d-c38a72244aa0
  weight: 100
  target: portainer:9443
- created_at: 1741754798.218
  updated_at: 1749260359.735
  tags: ~
  upstream: 14bdf793-231d-439e-8d67-3e47a3e7da05
  id: 7c5e8a1d-6022-42b6-83b7-b1f39739b9df
  weight: 100
  target: frontend:3001
- created_at: 1729739753.711
  updated_at: 1749260359.742
  tags: ~
  upstream: ab6fb6a2-97b9-45e7-b508-46df2df5203e
  id: 8d09a927-4def-479d-8ed9-0473be2a281a
  weight: 100
  target: hasura:8080
- created_at: 1729832116.752
  updated_at: 1749260359.72
  tags: ~
  upstream: 14ee49e7-f9bf-4234-a48f-7b7df7dda0ea
  id: a4cdac00-4421-4a49-b2ba-b2f720f4998d
  weight: 100
  target: keycloak:8080
- created_at: 1729739894.022
  updated_at: 1749260359.723
  tags: ~
  upstream: 420478e2-bdc8-49ec-ba0e-cc4cfd41afc8
  id: afe138dc-2d5e-4329-80ff-a0154b634f42
  weight: 100
  target: kibana:5601
- created_at: 1729739606.748
  updated_at: 1749260359.724
  tags: ~
  upstream: e6729ab9-c894-4963-bbdc-dfae17c88096
  id: e401fd6b-6a87-41be-a501-6456afeb14d3
  weight: 100
  target: backend:8080
- created_at: 1731459990.339
  updated_at: 1749260359.726
  tags: ~
  upstream: 09fa87e7-1cf1-4fc3-9ad6-6861572a8ef4
  id: e5f0e5f7-8b6a-476b-ac72-2e4a32bab571
  weight: 100
  target: minio:9000
- created_at: 1729914708.061
  updated_at: 1749260359.727
  tags: ~
  upstream: c0645f32-b63d-49ba-a723-5cbef9014c99
  id: ef3e3b44-a490-40e8-afac-2b9e2d202643
  weight: 100
  target: frontend:3000
- created_at: 1729914781.603
  updated_at: 1749260359.729
  tags: ~
  upstream: 747a43cc-42c4-457a-abac-518c2fe537b3
  id: f4e24691-4181-42a6-b32c-6c7e8cd92077
  weight: 100
  target: frontend:4000
- created_at: 1749280900.4
  updated_at: 1749280900.4
  tags: ~
  upstream: 3c840281-aeb3-4969-8ea4-cc12d45423a1
  id: f6a73dfa-96f1-4bd4-969f-a46592a5ac49
  weight: 100
  target: frontend:3002
keyauth_credentials:
- id: 6b9443ae-73f0-4db6-af00-4f1e3a415dbb
  created_at: 1734329245
  key: 4174348a-9222-4e81-b33e-5d72d2fd7f1e
  tags: ~
  ttl: ~
  consumer: 59d1ef15-24a5-4373-b957-e8192c15ff6e
- id: 6b979102-3576-4a98-b06e-ea72d25249d0
  created_at: 1749280821
  key: d763a82bd8154b58bd29d9cd141dcab0
  tags: ~
  ttl: ~
  consumer: 59d1ef15-24a5-4373-b957-e8192c15ff6e
- id: 90d62a81-2634-45a4-8384-6e155876db84
  created_at: 1749260555
  key: 33d911ac009240279e01b4a95655a0ad
  tags: ~
  ttl: ~
  consumer: 59d1ef15-24a5-4373-b957-e8192c15ff6e
