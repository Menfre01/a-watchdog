# config-store

## Purpose

定义网关配置的存储模型与生效语义:接入(providers)与配置(models / client_keys / settings)的分离、进程内一致性快照,以及写库即刷新的生效约定。

## Requirements

### Requirement: 接入与配置分离的数据模型

配置存储 **必须(MUST)** 包含相互独立的实体:`providers`(接入:协议类型、上游地址、凭据、启用状态)、`models`(模型目录:客户端可见名 → provider 引用 + 上游模型名 + 启用状态)、`client_keys`(客户端密钥)、`settings`(全局项)。模型目录对 provider 的引用 **必须** 通过稳定标识关联,**不得** 将 provider 的地址与凭据冗余进模型定义。

#### Scenario: 修改 provider 不影响模型目录

- **WHEN** 某 provider 的上游地址或凭据被修改
- **THEN** 引用它的模型条目无需改动即可使用新的地址与凭据

### Requirement: 快照与刷新语义

配置读取 **必须(MUST)** 通过进程内一致性快照完成;任何写入 **必须** 在落库成功后立即刷新快照,使后续新请求使用新配置;请求路径 **不得** 实时查询数据库获取配置。

#### Scenario: 配置修改即刻生效

- **WHEN** 某模型条目的映射配置被修改并保存
- **THEN** 之后到达的请求按新映射路由,无需重启进程

#### Scenario: 单次请求内视图一致

- **WHEN** 请求处理过程中多次读取配置
- **THEN** 读取均来自同一份内存快照,同一次请求内的视图一致

### Requirement: 持久化

所有配置 **必须(MUST)** 持久化于数据文件;进程重启后配置 **必须** 完整保留并保持原语义。

#### Scenario: 重启保留配置

- **WHEN** 网关进程重启
- **THEN** 已配置的 providers / models / client_keys / settings 全部保留并继续生效
