# Spec Delta

## ADDED Requirements

### Requirement: 统计口径拆分

轨迹统计卡 **必须(MUST)** 先给总计，再给 **主路** 与 **旁路** 的拆分。总计 **必须** 包含旁路。旁路组 **必须** 标注为总计的子集，界面 **不得** 让读者以为两组相加。

总计与拆分 **必须** 各自只出现一次：总计一行给 turns、steps、in/out/cache。拆分一行给主路与旁路的 steps 与 in/out/cache。

#### Scenario: 总计包含旁路

- **WHEN** 会话含旁路步
- **THEN** 总计 steps 等于主路 steps 与旁路 steps 之和，且旁路组标注为子集

#### Scenario: 拆分不重复总计

- **WHEN** 读者查看统计卡
- **THEN** turns 只在总计行出现，主路与旁路行只给各自的 steps 与 token

#### Scenario: 无旁路时不出现拆分

- **WHEN** 会话没有旁路步
- **THEN** 统计卡不显示旁路组，也不显示空的拆分行
