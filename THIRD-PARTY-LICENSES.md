# Third-Party Licenses

This file summarizes direct third-party dependencies used by distributed builds of this project.
It is an engineering compliance artifact and should be kept with Docker images, standalone binaries, frontend bundles, and Electron installers.

Scope: direct dependencies from `go.mod` (including the in-repo `relaykit` and `tokenkit` modules), `web/package.json`, and `electron/package.json`.
Transitive dependencies should be audited before a final external release.

## Dependency Inventory

| Area     | Scope       | Ecosystem | Dependency                                              | Version                              | License                 |
|----------|-------------|-----------|---------------------------------------------------------|--------------------------------------|-------------------------|
| backend  | production  | Go        | `github.com/Azure/go-ntlmssp`                           | `v0.1.1`                             | MIT                     |
| backend  | production  | Go        | `github.com/Calcium-Ion/go-epay`                        | `v0.0.5`                             | MIT                     |
| backend  | production  | Go        | `github.com/Calcium-Ion/moejs`                          | `v0.1.0-alpha.7`                     | Apache-2.0              |
| backend  | production  | Go        | `github.com/ClickHouse/clickhouse-go/v2`                | `v2.46.0`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/abema/go-mp4`                               | `v1.4.1`                             | MIT                     |
| backend  | test        | Go        | `github.com/alicebob/miniredis/v2`                      | `v2.38.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/andybalholm/brotli`                         | `v1.2.0`                             | MIT                     |
| backend  | production  | Go        | `github.com/anknown/ahocorasick`                        | `v0.0.0-20190904063843-d75dbd5169c0` | MIT                     |
| backend  | production  | Go        | `github.com/aws/aws-sdk-go-v2`                          | `v1.41.5`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream` | `v1.7.8`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/aws/aws-sdk-go-v2/credentials`              | `v1.19.10`                           | Apache-2.0              |
| backend  | production  | Go        | `github.com/aws/aws-sdk-go-v2/service/bedrockruntime`   | `v1.50.4`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/aws/smithy-go`                              | `v1.24.2`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/bytedance/gopkg`                            | `v0.1.3`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/casbin/casbin/v2`                           | `v2.135.0`                           | Apache-2.0              |
| backend  | production  | Go        | `github.com/coreos/go-oidc/v3`                          | `v3.21.0`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/expr-lang/expr`                             | `v1.17.8`                            | MIT                     |
| backend  | production  | Go        | `github.com/fxamacker/cbor/v2`                          | `v2.9.0`                             | MIT                     |
| backend  | production  | Go        | `github.com/gin-contrib/cors`                           | `v1.7.2`                             | MIT                     |
| backend  | production  | Go        | `github.com/gin-contrib/gzip`                           | `v0.0.6`                             | MIT                     |
| backend  | production  | Go        | `github.com/gin-contrib/static`                         | `v0.0.1`                             | MIT                     |
| backend  | production  | Go        | `github.com/gin-gonic/gin`                              | `v1.9.1`                             | MIT                     |
| backend  | production  | Go        | `github.com/glebarez/go-sqlite`                         | `v1.21.2`                            | BSD-3-Clause            |
| backend  | production  | Go        | `github.com/glebarez/sqlite`                            | `v1.11.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/go-audio/aiff`                              | `v1.1.0`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/go-audio/wav`                               | `v1.1.0`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/go-playground/validator/v10`                | `v10.20.0`                           | MIT                     |
| backend  | production  | Go        | `github.com/go-redis/redis/v8`                          | `v8.11.5`                            | BSD-2-Clause            |
| backend  | production  | Go        | `github.com/go-sql-driver/mysql`                        | `v1.7.0`                             | MPL-2.0                 |
| backend  | production  | Go        | `github.com/go-webauthn/webauthn`                       | `v0.14.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `github.com/golang-jwt/jwt/v5`                          | `v5.3.0`                             | MIT                     |
| backend  | production  | Go        | `github.com/google/uuid`                                | `v1.6.0`                             | BSD-3-Clause            |
| backend  | production  | Go        | `github.com/gorilla/websocket`                          | `v1.5.3`                             | BSD-2-Clause            |
| backend  | production  | Go        | `github.com/grafana/pyroscope-go`                       | `v1.2.7`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/jackc/pgx/v5`                               | `v5.9.2`                             | MIT                     |
| backend  | production  | Go        | `github.com/jfreymuth/oggvorbis`                        | `v1.0.5`                             | MIT                     |
| backend  | production  | Go        | `github.com/jinzhu/copier`                              | `v0.4.0`                             | MIT                     |
| backend  | production  | Go        | `github.com/joho/godotenv`                              | `v1.5.1`                             | MIT                     |
| backend  | test        | Go        | `github.com/json-iterator/go`                           | `v1.1.12`                            | MIT                     |
| backend  | production  | Go        | `github.com/klauspost/compress`                         | `v1.18.3`                            | BSD-3-Clause            |
| backend  | production  | Go        | `github.com/mewkiz/flac`                                | `v1.0.13`                            | Unlicense               |
| backend  | production  | Go        | `github.com/nicksnyder/go-i18n/v2`                      | `v2.6.1`                             | MIT                     |
| backend  | test        | Go        | `github.com/openai/openai-go`                           | `v1.12.0`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/pkg/errors`                                 | `v0.9.1`                             | BSD-2-Clause            |
| backend  | production  | Go        | `github.com/pquerna/otp`                                | `v1.5.0`                             | Apache-2.0              |
| backend  | production  | Go        | `github.com/prometheus/common`                          | `v0.62.0`                            | Apache-2.0              |
| backend  | production  | Go        | `github.com/samber/hot`                                 | `v0.11.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/samber/lo`                                  | `v1.53.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/shirou/gopsutil`                            | `v3.21.11+incompatible`              | BSD-3-Clause            |
| backend  | production  | Go        | `github.com/shopspring/decimal`                         | `v1.4.0`                             | MIT                     |
| backend  | test        | Go        | `github.com/stretchr/testify`                           | `v1.12.1`                            | MIT                     |
| backend  | production  | Go        | `github.com/stripe/stripe-go/v81`                       | `v81.4.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/tcolgate/mp3`                               | `v0.0.0-20170426193717-e79c5a46d300` | MIT                     |
| backend  | production  | Go        | `github.com/thanhpk/randstr`                            | `v1.0.6`                             | MIT                     |
| backend  | production  | Go        | `github.com/tidwall/gjson`                              | `v1.19.0`                            | MIT                     |
| backend  | production  | Go        | `github.com/tidwall/sjson`                              | `v1.2.5`                             | MIT                     |
| backend  | production  | Go        | `github.com/tiktoken-go/tokenizer`                      | `v0.8.1`                             | MIT                     |
| backend  | production  | Go        | `github.com/waffo-com/waffo-go`                         | `v1.3.2`                             | MIT                     |
| backend  | production  | Go        | `github.com/waffo-com/waffo-pancake-sdk-go`             | `v0.3.1`                             | MIT                     |
| backend  | production  | Go        | `github.com/yapingcat/gomedia`                          | `v0.0.0-20240906162731-17feea57090c` | MIT                     |
| backend  | production  | Go        | `golang.org/x/crypto`                                   | `v0.52.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/image`                                    | `v0.41.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/net`                                      | `v0.55.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/oauth2`                                   | `v0.36.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/sync`                                     | `v0.20.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/sys`                                      | `v0.45.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `golang.org/x/text`                                     | `v0.37.0`                            | BSD-3-Clause            |
| backend  | production  | Go        | `gopkg.in/yaml.v3`                                      | `v3.0.1`                             | Apache-2.0 OR MIT       |
| backend  | production  | Go        | `gorm.io/driver/clickhouse`                             | `v0.6.0`                             | MIT                     |
| backend  | production  | Go        | `gorm.io/driver/mysql`                                  | `v1.5.7`                             | MIT                     |
| backend  | production  | Go        | `gorm.io/driver/postgres`                               | `v1.5.9`                             | MIT                     |
| backend  | production  | Go        | `gorm.io/gorm`                                          | `v1.25.12`                           | MIT                     |
| web      | production  | npm       | `@base-ui/react`                                        | `1.6.0`                              | MIT                     |
| web      | production  | npm       | `@codemirror/lang-javascript`                           | `6.2.5`                              | MIT                     |
| web      | production  | npm       | `@codemirror/lang-markdown`                             | `6.5.1`                              | MIT                     |
| web      | production  | npm       | `@codemirror/language`                                  | `6.12.4`                             | MIT                     |
| web      | production  | npm       | `@codemirror/state`                                     | `6.7.1`                              | MIT                     |
| web      | production  | npm       | `@codemirror/view`                                      | `6.43.6`                             | MIT                     |
| web      | production  | npm       | `@fontsource-variable/lora`                             | `5.3.0`                              | OFL-1.1                 |
| web      | production  | npm       | `@fontsource-variable/public-sans`                      | `5.3.0`                              | OFL-1.1                 |
| web      | production  | npm       | `@hookform/resolvers`                                   | `5.4.0`                              | MIT                     |
| web      | production  | npm       | `@hugeicons/core-free-icons`                            | `4.2.2`                              | MIT                     |
| web      | production  | npm       | `@hugeicons/react`                                      | `1.1.9`                              | MIT                     |
| web      | production  | npm       | `@lezer/highlight`                                      | `1.2.3`                              | MIT                     |
| web      | production  | npm       | `@lobehub/icons`                                        | `5.22.0`                             | MIT                     |
| web      | production  | npm       | `@tanstack/react-query`                                 | `5.101.2`                            | MIT                     |
| web      | production  | npm       | `@tanstack/react-router`                                | `1.170.18`                           | MIT                     |
| web      | production  | npm       | `@tanstack/react-table`                                 | `8.21.3`                             | MIT                     |
| web      | production  | npm       | `@tanstack/react-virtual`                               | `3.14.6`                             | MIT                     |
| web      | production  | npm       | `@visactor/react-vchart`                                | `2.1.4`                              | MIT                     |
| web      | production  | npm       | `@visactor/vchart`                                      | `2.1.4`                              | MIT                     |
| web      | production  | npm       | `ai`                                                    | `7.0.136`                            | Apache-2.0              |
| web      | production  | npm       | `auto-skeleton-react`                                   | `1.0.5`                              | MIT                     |
| web      | production  | npm       | `axios`                                                 | `1.18.1`                             | MIT                     |
| web      | production  | npm       | `class-variance-authority`                              | `0.7.1`                              | Apache-2.0              |
| web      | production  | npm       | `clsx`                                                  | `2.1.1`                              | MIT                     |
| web      | production  | npm       | `cmdk`                                                  | `1.1.1`                              | MIT                     |
| web      | production  | npm       | `dayjs`                                                 | `1.11.21`                            | MIT                     |
| web      | production  | npm       | `dompurify`                                             | `3.4.16`                             | Apache-2.0 OR MPL-2.0   |
| web      | production  | npm       | `i18next`                                               | `26.3.6`                             | MIT                     |
| web      | production  | npm       | `i18next-browser-languagedetector`                      | `8.2.1`                              | MIT                     |
| web      | production  | npm       | `input-otp`                                             | `1.4.2`                              | MIT                     |
| web      | production  | npm       | `katex`                                                 | `0.18.2`                             | MIT                     |
| web      | production  | npm       | `lucide-react`                                          | `1.25.0`                             | ISC                     |
| web      | production  | npm       | `marked`                                                | `18.0.6`                             | MIT                     |
| web      | production  | npm       | `motion`                                                | `12.42.2`                            | MIT                     |
| web      | production  | npm       | `nanoid`                                                | `5.1.16`                             | MIT                     |
| web      | production  | npm       | `next-themes`                                           | `0.4.6`                              | MIT                     |
| web      | production  | npm       | `node-forge`                                            | `1.4.0`                              | BSD-3-Clause OR GPL-2.0 |
| web      | production  | npm       | `qrcode.react`                                          | `4.2.0`                              | ISC                     |
| web      | production  | npm       | `react`                                                 | `19.2.7`                             | MIT                     |
| web      | production  | npm       | `react-day-picker`                                      | `10.0.1`                             | MIT                     |
| web      | production  | npm       | `react-dom`                                             | `19.2.7`                             | MIT                     |
| web      | production  | npm       | `react-hook-form`                                       | `7.82.0`                             | MIT                     |
| web      | production  | npm       | `react-i18next`                                         | `17.0.10`                            | MIT                     |
| web      | production  | npm       | `react-icons`                                           | `5.7.0`                              | MIT                     |
| web      | production  | npm       | `react-resizable-panels`                                | `4.12.2`                             | MIT                     |
| web      | production  | npm       | `react-top-loading-bar`                                 | `3.0.2`                              | MIT                     |
| web      | production  | npm       | `recharts`                                              | `3.9.1`                              | MIT                     |
| web      | production  | npm       | `shiki`                                                 | `4.3.1`                              | MIT                     |
| web      | production  | npm       | `sonner`                                                | `2.0.7`                              | MIT                     |
| web      | production  | npm       | `sse.js`                                                | `2.8.0`                              | Apache-2.0              |
| web      | production  | npm       | `stream-markdown-parser`                                | `1.2.18`                             | MIT                     |
| web      | production  | npm       | `tailwind-merge`                                        | `3.6.0`                              | MIT                     |
| web      | production  | npm       | `tailwindcss`                                           | `4.3.3`                              | MIT                     |
| web      | production  | npm       | `tokenlens`                                             | `1.3.1`                              | MIT                     |
| web      | production  | npm       | `tw-animate-css`                                        | `1.4.0`                              | MIT                     |
| web      | production  | npm       | `use-stick-to-bottom`                                   | `1.1.6`                              | MIT                     |
| web      | production  | npm       | `vaul`                                                  | `1.1.2`                              | MIT                     |
| web      | production  | npm       | `yace`                                                  | `1.1.1`                              | MIT                     |
| web      | production  | npm       | `yaml`                                                  | `2.9.0`                              | ISC                     |
| web      | production  | npm       | `zod`                                                   | `4.4.3`                              | MIT                     |
| web      | production  | npm       | `zustand`                                               | `5.0.14`                             | MIT                     |
| web      | development | npm       | `@rsbuild/core`                                         | `2.1.6`                              | MIT                     |
| web      | development | npm       | `@rsbuild/plugin-react`                                 | `2.1.0`                              | MIT                     |
| web      | development | npm       | `@rsbuild/plugin-tailwindcss`                           | `2.0.3`                              | MIT                     |
| web      | development | npm       | `@tanstack/react-query-devtools`                        | `5.101.2`                            | MIT                     |
| web      | development | npm       | `@tanstack/react-router-devtools`                       | `1.167.0`                            | MIT                     |
| web      | development | npm       | `@tanstack/router-plugin`                               | `1.168.42`                           | MIT                     |
| web      | development | npm       | `@testing-library/jest-dom`                             | `7.0.0`                              | MIT                     |
| web      | development | npm       | `@testing-library/react`                                | `16.3.2`                             | MIT                     |
| web      | development | npm       | `@testing-library/user-event`                           | `14.6.1`                             | MIT                     |
| web      | development | npm       | `@types/node`                                           | `26.1.1`                             | MIT                     |
| web      | development | npm       | `@types/node-forge`                                     | `1.3.14`                             | MIT                     |
| web      | development | npm       | `@types/react`                                          | `19.2.17`                            | MIT                     |
| web      | development | npm       | `@types/react-dom`                                      | `19.2.3`                             | MIT                     |
| web      | development | npm       | `@xyflow/react`                                         | `12.11.2`                            | MIT                     |
| web      | development | npm       | `embla-carousel-react`                                  | `8.6.0`                              | MIT                     |
| web      | development | npm       | `jsdom`                                                 | `29.1.1`                             | MIT                     |
| web      | development | npm       | `knip`                                                  | `6.27.0`                             | ISC                     |
| web      | development | npm       | `oxfmt`                                                 | `0.72.0`                             | MIT                     |
| web      | development | npm       | `oxlint`                                                | `1.74.0`                             | MIT                     |
| web      | development | npm       | `shadcn`                                                | `4.13.1`                             | MIT                     |
| web      | development | npm       | `vitest`                                                | `4.1.10`                             | MIT                     |
| electron | development | npm       | `cross-env`                                             | `7.0.3`                              | MIT                     |
| electron | development | npm       | `electron`                                              | `41.10.6`                            | MIT                     |
| electron | development | npm       | `electron-builder`                                      | `26.15.3`                            | MIT                     |

## Codex 模型目录数据

`controller/codex_models.json` 原样收录 OpenAI Codex 的 `codex-rs/models-manager/models.json`，
来源版本为 `29091c7234b0e58d4e5474841dd69474d6116ce0`（Apache-2.0）。
运行时补充旧版客户端的 `base_instructions` 字段，并关闭专用传输能力标志；源快照保持原样。

上游版权声明：OpenAI Codex，Copyright 2025 OpenAI。
上游地址：https://github.com/openai/codex

目录数据适用的上游完整许可（保留法律原文）：

```text
                                 Apache License
                           Version 2.0, January 2004
                        http://www.apache.org/licenses/

TERMS AND CONDITIONS FOR USE, REPRODUCTION, AND DISTRIBUTION

1.  Definitions.

    "License" shall mean the terms and conditions for use, reproduction,
    and distribution as defined by Sections 1 through 9 of this document.

    "Licensor" shall mean the copyright owner or entity authorized by
    the copyright owner that is granting the License.

    "Legal Entity" shall mean the union of the acting entity and all
    other entities that control, are controlled by, or are under common
    control with that entity. For the purposes of this definition,
    "control" means (i) the power, direct or indirect, to cause the
    direction or management of such entity, whether by contract or
    otherwise, or (ii) ownership of fifty percent (50%) or more of the
    outstanding shares, or (iii) beneficial ownership of such entity.

    "You" (or "Your") shall mean an individual or Legal Entity
    exercising permissions granted by this License.

    "Source" form shall mean the preferred form for making modifications,
    including but not limited to software source code, documentation
    source, and configuration files.

    "Object" form shall mean any form resulting from mechanical
    transformation or translation of a Source form, including but
    not limited to compiled object code, generated documentation,
    and conversions to other media types.

    "Work" shall mean the work of authorship, whether in Source or
    Object form, made available under the License, as indicated by a
    copyright notice that is included in or attached to the work
    (an example is provided in the Appendix below).

    "Derivative Works" shall mean any work, whether in Source or Object
    form, that is based on (or derived from) the Work and for which the
    editorial revisions, annotations, elaborations, or other modifications
    represent, as a whole, an original work of authorship. For the purposes
    of this License, Derivative Works shall not include works that remain
    separable from, or merely link (or bind by name) to the interfaces of,
    the Work and Derivative Works thereof.

    "Contribution" shall mean any work of authorship, including
    the original version of the Work and any modifications or additions
    to that Work or Derivative Works thereof, that is intentionally
    submitted to Licensor for inclusion in the Work by the copyright owner
    or by an individual or Legal Entity authorized to submit on behalf of
    the copyright owner. For the purposes of this definition, "submitted"
    means any form of electronic, verbal, or written communication sent
    to the Licensor or its representatives, including but not limited to
    communication on electronic mailing lists, source code control systems,
    and issue tracking systems that are managed by, or on behalf of, the
    Licensor for the purpose of discussing and improving the Work, but
    excluding communication that is conspicuously marked or otherwise
    designated in writing by the copyright owner as "Not a Contribution."

    "Contributor" shall mean Licensor and any individual or Legal Entity
    on behalf of whom a Contribution has been received by Licensor and
    subsequently incorporated within the Work.

2.  Grant of Copyright License. Subject to the terms and conditions of
    this License, each Contributor hereby grants to You a perpetual,
    worldwide, non-exclusive, no-charge, royalty-free, irrevocable
    copyright license to reproduce, prepare Derivative Works of,
    publicly display, publicly perform, sublicense, and distribute the
    Work and such Derivative Works in Source or Object form.

3.  Grant of Patent License. Subject to the terms and conditions of
    this License, each Contributor hereby grants to You a perpetual,
    worldwide, non-exclusive, no-charge, royalty-free, irrevocable
    (except as stated in this section) patent license to make, have made,
    use, offer to sell, sell, import, and otherwise transfer the Work,
    where such license applies only to those patent claims licensable
    by such Contributor that are necessarily infringed by their
    Contribution(s) alone or by combination of their Contribution(s)
    with the Work to which such Contribution(s) was submitted. If You
    institute patent litigation against any entity (including a
    cross-claim or counterclaim in a lawsuit) alleging that the Work
    or a Contribution incorporated within the Work constitutes direct
    or contributory patent infringement, then any patent licenses
    granted to You under this License for that Work shall terminate
    as of the date such litigation is filed.

4.  Redistribution. You may reproduce and distribute copies of the
    Work or Derivative Works thereof in any medium, with or without
    modifications, and in Source or Object form, provided that You
    meet the following conditions:

    (a) You must give any other recipients of the Work or
    Derivative Works a copy of this License; and

    (b) You must cause any modified files to carry prominent notices
    stating that You changed the files; and

    (c) You must retain, in the Source form of any Derivative Works
    that You distribute, all copyright, patent, trademark, and
    attribution notices from the Source form of the Work,
    excluding those notices that do not pertain to any part of
    the Derivative Works; and

    (d) If the Work includes a "NOTICE" text file as part of its
    distribution, then any Derivative Works that You distribute must
    include a readable copy of the attribution notices contained
    within such NOTICE file, excluding those notices that do not
    pertain to any part of the Derivative Works, in at least one
    of the following places: within a NOTICE text file distributed
    as part of the Derivative Works; within the Source form or
    documentation, if provided along with the Derivative Works; or,
    within a display generated by the Derivative Works, if and
    wherever such third-party notices normally appear. The contents
    of the NOTICE file are for informational purposes only and
    do not modify the License. You may add Your own attribution
    notices within Derivative Works that You distribute, alongside
    or as an addendum to the NOTICE text from the Work, provided
    that such additional attribution notices cannot be construed
    as modifying the License.

    You may add Your own copyright statement to Your modifications and
    may provide additional or different license terms and conditions
    for use, reproduction, or distribution of Your modifications, or
    for any such Derivative Works as a whole, provided Your use,
    reproduction, and distribution of the Work otherwise complies with
    the conditions stated in this License.

5.  Submission of Contributions. Unless You explicitly state otherwise,
    any Contribution intentionally submitted for inclusion in the Work
    by You to the Licensor shall be under the terms and conditions of
    this License, without any additional terms or conditions.
    Notwithstanding the above, nothing herein shall supersede or modify
    the terms of any separate license agreement you may have executed
    with Licensor regarding such Contributions.

6.  Trademarks. This License does not grant permission to use the trade
    names, trademarks, service marks, or product names of the Licensor,
    except as required for reasonable and customary use in describing the
    origin of the Work and reproducing the content of the NOTICE file.

7.  Disclaimer of Warranty. Unless required by applicable law or
    agreed to in writing, Licensor provides the Work (and each
    Contributor provides its Contributions) on an "AS IS" BASIS,
    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
    implied, including, without limitation, any warranties or conditions
    of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS FOR A
    PARTICULAR PURPOSE. You are solely responsible for determining the
    appropriateness of using or redistributing the Work and assume any
    risks associated with Your exercise of permissions under this License.

8.  Limitation of Liability. In no event and under no legal theory,
    whether in tort (including negligence), contract, or otherwise,
    unless required by applicable law (such as deliberate and grossly
    negligent acts) or agreed to in writing, shall any Contributor be
    liable to You for damages, including any direct, indirect, special,
    incidental, or consequential damages of any character arising as a
    result of this License or out of the use or inability to use the
    Work (including but not limited to damages for loss of goodwill,
    work stoppage, computer failure or malfunction, or any and all
    other commercial damages or losses), even if such Contributor
    has been advised of the possibility of such damages.

9.  Accepting Warranty or Additional Liability. While redistributing
    the Work or Derivative Works thereof, You may choose to offer,
    and charge a fee for, acceptance of support, warranty, indemnity,
    or other liability obligations and/or rights consistent with this
    License. However, in accepting such obligations, You may act only
    on Your own behalf and on Your sole responsibility, not on behalf
    of any other Contributor, and only if You agree to indemnify,
    defend, and hold each Contributor harmless for any liability
    incurred by, or claims asserted against, such Contributor by reason
    of your accepting any such warranty or additional liability.

END OF TERMS AND CONDITIONS

APPENDIX: How to apply the Apache License to your work.

      To apply the Apache License to your work, attach the following
      boilerplate notice, with the fields enclosed by brackets "[]"
      replaced with your own identifying information. (Don't include
      the brackets!)  The text should be enclosed in the appropriate
      comment syntax for the file format. We also recommend that a
      file or class name and description of purpose be included on the
      same "printed page" as the copyright notice for easier
      identification within third-party archives.

Copyright 2025 OpenAI

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
```

## License Texts

### Apache-2.0

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

### Apache-2.0 OR MIT

Dual-licensed components may be used under Apache-2.0 or MIT. Both standard license texts are included below.

Apache License
Version 2.0, January 2004
https://www.apache.org/licenses/

Licensed under the Apache License, Version 2.0 (the "License"); you may not
use this file except in compliance with the License. You may obtain a copy of
the License at:

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS, WITHOUT
WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied. See the
License for the specific language governing permissions and limitations under
the License.

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### BSD-2-Clause

BSD 2-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### BSD-3-Clause

BSD 3-Clause License

Redistribution and use in source and binary forms, with or without
modification, are permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this
   list of conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice,
   this list of conditions and the following disclaimer in the documentation
   and/or other materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors
   may be used to endorse or promote products derived from this software
   without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE
FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR
SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER
CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY,
OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.

### ISC

ISC License

Permission to use, copy, modify, and/or distribute this software for any
purpose with or without fee is hereby granted, provided that the above
copyright notice and this permission notice appear in all copies.

THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES WITH
REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF MERCHANTABILITY
AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR ANY SPECIAL, DIRECT,
INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES WHATSOEVER RESULTING FROM
LOSS OF USE, DATA OR PROFITS, WHETHER IN AN ACTION OF CONTRACT, NEGLIGENCE OR
OTHER TORTIOUS ACTION, ARISING OUT OF OR IN CONNECTION WITH THE USE OR
PERFORMANCE OF THIS SOFTWARE.

### MIT

MIT License

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### MPL-2.0

Mozilla Public License 2.0
https://mozilla.org/MPL/2.0/

The source code of the MPL-covered files is available from the upstream project at https://github.com/go-sql-driver/mysql.

### OFL-1.1

SIL Open Font License 1.1

The font dependency listed under OFL-1.1 is licensed under the SIL Open Font
License, Version 1.1. The full license text is available at:
https://openfontlicense.org/open-font-license-official-text/

When distributing font files, preserve the OFL license text, copyright notices,
and reserved font name restrictions supplied by the upstream font project.

### Unlicense

The Unlicense

This is free and unencumbered software released into the public domain.
Anyone is free to copy, modify, publish, use, compile, sell, or distribute
this software, either in source code form or as a compiled binary, for any
purpose, commercial or non-commercial, and by any means.

For more information, please refer to https://unlicense.org/
