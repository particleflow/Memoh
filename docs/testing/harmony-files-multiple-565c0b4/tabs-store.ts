import {defineStore} from 'pinia'
export const useWorkspaceTabsStore=defineStore('files-test-tabs',{state:()=>({activeId:null}),actions:{openFile(){},openFilePinned(){},openFileToSide(){}}})
