package server

import (
	"AdaptixServer/core/adaptix"
	"AdaptixServer/core/utils/krypt"
	"AdaptixServer/core/utils/logs"
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

func (ts *Teamserver) TsTaskCreate(agentId string, cmdline string, client string, taskObject []byte) {
	var (
		agent    *Agent
		taskData adaptix.TaskData
		value    any
		ok       bool
		err      error
	)
	err = json.Unmarshal(taskObject, &taskData)
	if err != nil {
		logs.Error("", "TsTaskCreate: %v", err.Error())
		return
	}

	value, ok = ts.agents.Get(agentId)
	if !ok {
		logs.Error("", "TsTaskCreate: agent %v not found", agentId)
		return
	}
	agent = value.(*Agent)

	//err = json.Unmarshal(dataMessage, &messageData)
	//if err != nil {
	//	return err
	//}

	if taskData.TaskId == "" {
		taskData.TaskId, _ = krypt.GenerateUID(8)
	}
	taskData.AgentId = agentId
	taskData.CommandLine = cmdline
	taskData.Client = client
	taskData.Computer = agent.Data.Computer
	taskData.StartDate = time.Now().Unix()
	if taskData.Completed {
		taskData.FinishDate = taskData.StartDate
	}

	switch taskData.Type {

	case TYPE_TASK:
		if taskData.Sync {
			packet := CreateSpAgentTaskSync(taskData)
			ts.TsSyncAllClients(packet)

			packet2 := CreateSpAgentConsoleTaskSync(taskData)
			ts.TsSyncAllClients(packet2)

			agent.OutConsole.Put(packet2)
			_ = ts.DBMS.DbConsoleInsert(agentId, packet2)
		}
		agent.TasksQueue.Put(taskData)

	case TYPE_BROWSER:
		agent.TasksQueue.Put(taskData)

	case TYPE_JOB:
		if taskData.Sync {
			packet := CreateSpAgentTaskSync(taskData)
			ts.TsSyncAllClients(packet)

			packet2 := CreateSpAgentConsoleTaskSync(taskData)
			ts.TsSyncAllClients(packet2)

			agent.OutConsole.Put(packet2)
			_ = ts.DBMS.DbConsoleInsert(agentId, packet2)
		}
		agent.TasksQueue.Put(taskData)

	case TYPE_TUNNEL:
		if taskData.Sync {
			agent.RunningTasks.Put(taskData.TaskId, taskData)
		} else {
			agent.TunnelQueue.Put(taskData)
		}

	default:
		break
	}

	//if len(messageData.Message) > 0 || len(messageData.Text) > 0 {
	//	ts.TsAgentConsoleOutput(agentId, messageData.Status, messageData.Message, messageData.Text)
	//}
}

func (ts *Teamserver) TsTaskUpdate(agentId string, taskObject []byte) {
	var (
		agent    *Agent
		task     adaptix.TaskData
		taskData adaptix.TaskData
		value    any
		ok       bool
		err      error
	)
	err = json.Unmarshal(taskObject, &taskData)
	if err != nil {
		return
	}

	value, ok = ts.agents.Get(agentId)
	if !ok {
		logs.Error("", "TsTaskUpdate: agent %v not found", agentId)
		return
	}
	agent = value.(*Agent)

	value, ok = agent.RunningTasks.GetDelete(taskData.TaskId)
	if !ok {
		return
	}
	task = value.(adaptix.TaskData)

	task.Data = []byte("")
	task.FinishDate = taskData.FinishDate
	task.Completed = taskData.Completed

	if task.Type == TYPE_JOB {
		if task.MessageType != CONSOLE_OUT_ERROR {
			task.MessageType = taskData.MessageType
		}

		var oldMessage string
		if task.Message == "" {
			oldMessage = taskData.Message
		} else {
			oldMessage = task.Message
		}

		oldText := task.ClearText

		task.Message = taskData.Message
		task.ClearText = taskData.ClearText

		packet := CreateSpAgentTaskUpdate(task)
		packet2 := CreateSpAgentConsoleTaskUpd(task)

		task.Message = oldMessage
		task.ClearText = oldText + task.ClearText

		if task.Completed {
			agent.CompletedTasks.Put(task.TaskId, task)
		} else {
			agent.RunningTasks.Put(task.TaskId, task)
		}

		if task.Sync {
			if task.Completed {
				_ = ts.DBMS.DbTaskInsert(task)
			}

			ts.TsSyncAllClients(packet)
			ts.TsSyncAllClients(packet2)

			agent.OutConsole.Put(packet2)
			_ = ts.DBMS.DbConsoleInsert(agentId, packet2)
		}

	} else if task.Type == TYPE_TUNNEL {
		if task.Completed {
			task.Message = taskData.Message
			task.MessageType = taskData.MessageType

			agent.CompletedTasks.Put(task.TaskId, task)

			if task.Sync {
				_ = ts.DBMS.DbTaskInsert(task)

				packet := CreateSpAgentTaskUpdate(task)
				ts.TsSyncAllClients(packet)

				packet2 := CreateSpAgentConsoleTaskUpd(task)
				ts.TsSyncAllClients(packet2)

				agent.OutConsole.Put(packet2)
				_ = ts.DBMS.DbConsoleInsert(agentId, packet2)
			}
		}

	} else if task.Type == TYPE_TASK || task.Type == TYPE_BROWSER {
		task.MessageType = taskData.MessageType
		task.Message = taskData.Message
		task.ClearText = taskData.ClearText

		if task.Completed {
			agent.CompletedTasks.Put(task.TaskId, task)
		} else {
			agent.RunningTasks.Put(task.TaskId, task)
		}

		if task.Sync {
			if task.Completed {
				_ = ts.DBMS.DbTaskInsert(task)
			}

			packet := CreateSpAgentTaskUpdate(task)
			ts.TsSyncAllClients(packet)

			packet2 := CreateSpAgentConsoleTaskUpd(task)
			ts.TsSyncAllClients(packet2)

			agent.OutConsole.Put(packet2)
			_ = ts.DBMS.DbConsoleInsert(agentId, packet2)
		}
	}
}

/////

func (ts *Teamserver) TsTaskQueueGetAvailable(agentId string, availableSize int) ([][]byte, error) {
	var (
		tasksArray [][]byte
		agent      *Agent
		value      any
		ok         bool
	)

	value, ok = ts.agents.Get(agentId)
	if ok {
		agent = value.(*Agent)
	} else {
		return nil, fmt.Errorf("TsTaskQueueGetAvailable: agent %v not found", agentId)
	}

	/// TASKS QUEUE

	for i := 0; i < agent.TasksQueue.Len(); i++ {
		value, ok = agent.TasksQueue.Get(i)
		if ok {
			taskData := value.(adaptix.TaskData)
			if len(tasksArray)+len(taskData.Data) < availableSize {
				var taskBuffer bytes.Buffer
				_ = json.NewEncoder(&taskBuffer).Encode(taskData)
				tasksArray = append(tasksArray, taskBuffer.Bytes())
				agent.RunningTasks.Put(taskData.TaskId, taskData)
				agent.TasksQueue.Delete(i)
				i--
			} else {
				break
			}
		} else {
			break
		}
	}

	/// TUNNELS QUEUE

	for i := 0; i < agent.TunnelQueue.Len(); i++ {
		value, ok = agent.TunnelQueue.Get(i)
		if ok {
			tunnelTaskData := value.(adaptix.TaskData)
			if len(tasksArray)+len(tunnelTaskData.Data) < availableSize {
				var taskBuffer bytes.Buffer
				_ = json.NewEncoder(&taskBuffer).Encode(tunnelTaskData)
				tasksArray = append(tasksArray, taskBuffer.Bytes())
				agent.TunnelQueue.Delete(i)
				i--
			} else {
				break
			}
		} else {
			break
		}
	}

	return tasksArray, nil
}

func (ts *Teamserver) TsTaskStop(agentId string, taskId string) error {
	var (
		agent *Agent
		task  adaptix.TaskData
		value any
		ok    bool
		found bool
	)

	value, ok = ts.agents.Get(agentId)
	if ok {
		agent = value.(*Agent)
	} else {
		return fmt.Errorf("agent %v not found", agentId)
	}

	found = false
	for i := 0; i < agent.TasksQueue.Len(); i++ {
		if value, ok = agent.TasksQueue.Get(i); ok {
			task = value.(adaptix.TaskData)
			if task.TaskId == taskId {
				agent.TasksQueue.Delete(i)
				found = true
				break
			}
		}
	}

	if found {
		packet := CreateSpAgentTaskRemove(task)
		ts.TsSyncAllClients(packet)
		return nil
	}

	value, ok = agent.RunningTasks.Get(taskId)
	if ok {
		task = value.(adaptix.TaskData)
		if task.Type == TYPE_JOB {
			data, err := ts.Extender.ExAgentBrowserJobKill(agent.Data.Name, taskId)
			if err != nil {
				return err
			}

			ts.TsTaskCreate(agent.Data.Id, "job kill "+taskId, "", data)

			return nil
		} else {
			return fmt.Errorf("taski %v in process", taskId)
		}
	}
	return nil
}

func (ts *Teamserver) TsTaskDelete(agentId string, taskId string) error {
	var (
		agent *Agent
		task  adaptix.TaskData
		value any
		ok    bool
	)

	value, ok = ts.agents.Get(agentId)
	if ok {
		agent = value.(*Agent)
	} else {
		return fmt.Errorf("agent %v not found", agentId)
	}

	for i := 0; i < agent.TasksQueue.Len(); i++ {
		if value, ok = agent.TasksQueue.Get(i); ok {
			task = value.(adaptix.TaskData)
			if task.TaskId == taskId {
				return fmt.Errorf("task %v in process", taskId)
			}
		}
	}

	value, ok = agent.RunningTasks.Get(taskId)
	if ok {
		return fmt.Errorf("task %v in process", taskId)
	}

	value, ok = agent.CompletedTasks.GetDelete(taskId)
	if ok {
		task = value.(adaptix.TaskData)
		ts.DBMS.DbTaskDelete(task.TaskId, "")

		packet := CreateSpAgentTaskRemove(task)
		ts.TsSyncAllClients(packet)
		return nil
	}

	return fmt.Errorf("task %v not found", taskId)
}
