let inputAccess=null,outputAccess=null;
let inputBuffer=null;

async function initFiles(inputName,outputName){
  const root=await navigator.storage.getDirectory();
  const ih=await root.getFileHandle(inputName);
  const oh=await root.getFileHandle(outputName,{create:true});
  inputAccess=await ih.createSyncAccessHandle();
  outputAccess=await oh.createSyncAccessHandle();
  globalThis.rvzInputSize=()=>inputAccess.getSize();
  globalThis.rvzInputRead=(offset,length)=>{
    if(!inputBuffer || inputBuffer.byteLength<length) inputBuffer=new Uint8Array(length);
    const n=inputAccess.read(inputBuffer,{at:Number(offset)});
    return n===length?inputBuffer:inputBuffer.slice(0,n);
  };
  globalThis.rvzOutputWrite=(data,offset)=>outputAccess.write(data,{at:Number(offset)});
  globalThis.rvzProgress=(done,total)=>postMessage({type:'progress',done,total});
}

let wasmReady=false;
(async()=>{
  try{
    importScripts('./wasm_exec.js');
    const go=new Go();
    const result=await WebAssembly.instantiateStreaming(fetch('./rvz.wasm'),go.importObject);
    go.run(result.instance);
    wasmReady=true;
    postMessage({type:'ready'});
  }catch(e){postMessage({type:'error',message:'Decoder failed to load: '+e.message});}
})();

onmessage=async e=>{
  const m=e.data||{};
  if(m.type!=='start'||!wasmReady)return;
  try{
    await initFiles(m.input,m.output);
    const result=globalThis.rvzConvert();
    if(result.error)throw new Error(result.error);
    outputAccess.flush();
    outputAccess.close();inputAccess.close();
    postMessage({type:'done',size:result.size,name:m.name});
  }catch(e){
    try{if(outputAccess)outputAccess.close()}catch(_){}
    try{if(inputAccess)inputAccess.close()}catch(_){}
    postMessage({type:'error',message:e.message||String(e)});
  }
};
