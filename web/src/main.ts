import { createApp } from 'vue'
import './styles/tokens.css'
import './styles/base.css'
import App from './App.vue'
import { syncMeta } from './theme'

createApp(App).mount('#app')
// 样式表生效后补 theme-color:内联脚本只定属性,色值只存在于令牌文件。
syncMeta()
