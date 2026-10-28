import {createApp,h,ref} from 'vue'
import {createI18n} from 'vue-i18n'
import AttachmentBlock from './src/pages/home/components/attachment-block.vue'
import Gallery from './src/pages/home/components/media-gallery-lightbox.vue'
import {originalBlob} from './src/lib/harmony-files'
import {resolveUrl} from './src/pages/home/composables/useMediaGallery'
import {setupApiClient} from './src/lib/api-client'
import zh from './src/i18n/locales/zh.json'
import en from './src/i18n/locales/en.json'
import './src/style.css'
localStorage.setItem('token','synthetic-isolated-token')
setupApiClient({baseUrl:location.origin+'/api'})
const bot='10000000-0000-4000-8000-000000000001',session='20000000-0000-4000-8000-000000000002',delegation='30000000-0000-4000-8000-000000000003'
const path=(hash:string)=>`/bots/${bot}/sessions/${session}/delegations/${delegation}/attachments/${hash}`
const failed={type:'image',name:'original.png',mime:'image/png',bot_id:bot,content_hash:'a'.repeat(64),metadata:{delegation_url:path('a'.repeat(64))}}
const healthyBytes=Uint8Array.from(atob('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAusB9Wl6r9sAAAAASUVORK5CYII='),c=>c.charCodeAt(0))
const hash=Array.from(new Uint8Array(await crypto.subtle.digest('SHA-256',healthyBytes)),b=>b.toString(16).padStart(2,'0')).join('')
const healthy={...failed,name:'available.png',content_hash:hash,metadata:{delegation_url:path(hash)}}
const items=[failed,healthy].map(att=>({src:resolveUrl(att),name:att.name,type:'image' as const,original:()=>originalBlob(att)}))
const index=ref<number|null>(null); const original=JSON.stringify([failed,healthy])
window.fixture={openFailed:()=>{index.value=0},snapshot:()=>JSON.stringify([failed,healthy]),original,healthyHash:hash}
createApp({render:()=>h('main',{class:'p-4 space-y-4 text-foreground bg-background min-h-screen'},[
 ...(['content','attachment'] as const).map(variant=>h('section',{'data-variant':variant},[h(AttachmentBlock,{variant,block:{id:1,type:'attachments',attachments:[failed]},onOpenMedia:()=>{index.value=0}})])),
 h('section',{'data-variant':'available'},[h(AttachmentBlock,{variant:'content',block:{id:2,type:'attachments',attachments:[healthy]},onOpenMedia:()=>{index.value=1}})]),
 h(Gallery,{items,openIndex:index.value,'onUpdate:openIndex':value=>{index.value=value}})
])}).use(createI18n({legacy:false,locale:'zh',messages:{zh,en}})).mount('#app')
