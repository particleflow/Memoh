import {defineStore} from 'pinia'
export const useChatStore=defineStore('files-test-chat',{state:()=>({fsChangedAt:0}),actions:{markFsChanged(){this.fsChangedAt++}}})
