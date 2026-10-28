import {createApp,h,reactive} from 'vue'
import {createPinia} from 'pinia'
import {createI18n} from 'vue-i18n'
import FilesPane from './src/components/sidebar/files-pane.vue'
import {setupApiClient} from './src/lib/api-client'
import zh from './src/i18n/locales/zh.json'
import en from './src/i18n/locales/en.json'
import './src/style.css'
setupApiClient({baseUrl:location.origin+'/api'})
localStorage.setItem('token','synthetic-isolated-token')
const props=reactive({botId:'10000000-0000-4000-8000-000000000001',botName:'Synthetic files',active:true,canWrite:true})
createApp({render:()=>h('main',{class:'p-4 h-screen min-w-0 bg-background text-foreground'},h(FilesPane,props))}).use(createPinia()).use(createI18n({legacy:false,locale:'zh',messages:{zh,en}})).mount('#app')
