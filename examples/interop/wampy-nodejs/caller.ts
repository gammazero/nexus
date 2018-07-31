
import {Broker} from "./broker"

function main() {
    let broker = new Broker({port:8008})
    parallelCall(broker,10*1000)

}


function parallelCall(broker:Broker, duration: number) {

    let promises = []
    let st = Date.now(), lt = st
    while (Date.now()-st<duration) {
        if (Date.now()-lt>1000) {
            let cps = Math.round(promises.length*1000 / (Date.now()-st))
            console.log( "Calls per second: ", cps ,"calls/sec [",promises.length," calls]");
            lt = Date.now()

        }
        promises.push(broker.rpc("server.time").catch(err=>0))
    }
    let cps = Math.round(promises.length*1000 / duration)
    console.log( "Calls per second: ", cps ,"calls/sec [",promises.length," calls]");
    return Promise.all(promises).then(_=>{

        duration = Date.now()-st
        let cps = Math.round(promises.length*1000 / duration)
        console.log( "All calls finished in", duration ,"ms - That's ",cps,"calls/sec");

    })

}

main();