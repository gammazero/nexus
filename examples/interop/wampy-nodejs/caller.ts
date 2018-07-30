
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
            console.log(new Date(),promises.length,"calls")
            lt = Date.now()

        }
        promises.push(broker.rpc("server.time").catch(err=>0))
    }
    console.log( "Call per second: ", promises.length / duration,"obj/sec");
    return Promise.all(promises)

}

main();