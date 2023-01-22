for i in {1..2}
do
   echo "Running integration test $i time:"
   make integration-test
   if [ $? -ne 0 ];
   then
    echo "Test fails on $1 attempt"
    exit
   fi
done