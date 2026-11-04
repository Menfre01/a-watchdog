# Spec Delta

## MODIFIED Requirements

### Requirement: 配置管理能力

控制台 **必须(MUST)** 提供 providers、models(模型目录)、client_keys 的增删改查。对 client_keys **必须** 支持新建密钥与停用。对 providers **必须** 提供「测试连接」。

settings **不得** 出现在控制台界面。settings 的读写 **必须** 只经 HTTP 接口进行，接口与数据保留。

#### Scenario: 完成一次接入配置

- **WHEN** 管理员新建 provider、新建引用它的模型条目、新建一个客户端 key
- **THEN** 系统即时按新配置服务请求

#### Scenario: 停用密钥

- **WHEN** 管理员停用某个 client key
- **THEN** 该 key 随即失效

#### Scenario: 配置模块不含全局配置分页

- **WHEN** 管理员打开配置模块
- **THEN** 分页只有接入、模型目录、客户端密钥,没有全局配置
