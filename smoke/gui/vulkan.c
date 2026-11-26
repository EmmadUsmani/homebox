#include <assert.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <vulkan/vulkan.h>

#define VK(call) assert((call) == VK_SUCCESS)

int main(int argc, char **argv) {
    assert(argc == 2);
    FILE *file = fopen(argv[1], "rb");
    assert(file);
    uint32_t code[1024];
    size_t code_size = fread(code, 1, sizeof code, file);
    assert(feof(file) && code_size && code_size % 4 == 0);
    fclose(file);

    VkInstance instance;
    VK(vkCreateInstance(&(VkInstanceCreateInfo){
        .sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO
    }, NULL, &instance));
    uint32_t device_count = 1;
    VkPhysicalDevice physical;
    VK(vkEnumeratePhysicalDevices(instance, &device_count, &physical));
    assert(device_count == 1);
    VkPhysicalDeviceProperties properties;
    vkGetPhysicalDeviceProperties(physical, &properties);
    assert(properties.deviceType == VK_PHYSICAL_DEVICE_TYPE_CPU &&
           strstr(properties.deviceName, "llvmpipe"));
    uint32_t family_count = 0;
    vkGetPhysicalDeviceQueueFamilyProperties(physical, &family_count, NULL);
    assert(family_count > 0);
    VkQueueFamilyProperties families[family_count];
    vkGetPhysicalDeviceQueueFamilyProperties(physical, &family_count, families);
    uint32_t family = 0;
    while (family < family_count && !(families[family].queueFlags & VK_QUEUE_COMPUTE_BIT))
        family++;
    assert(family < family_count);
    float priority = 1.0f;
    VkDevice device;
    VK(vkCreateDevice(physical, &(VkDeviceCreateInfo){
        .sType = VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO,
        .queueCreateInfoCount = 1,
        .pQueueCreateInfos = &(VkDeviceQueueCreateInfo){
            .sType = VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO,
            .queueFamilyIndex = family, .queueCount = 1, .pQueuePriorities = &priority
        }
    }, NULL, &device));
    VkQueue queue;
    vkGetDeviceQueue(device, family, 0, &queue);

    const VkDeviceSize size = 4 * sizeof(uint32_t);
    VkBuffer buffer;
    VK(vkCreateBuffer(device, &(VkBufferCreateInfo){
        .sType = VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO,
        .size = size, .usage = VK_BUFFER_USAGE_STORAGE_BUFFER_BIT
    }, NULL, &buffer));
    VkMemoryRequirements requirements;
    vkGetBufferMemoryRequirements(device, buffer, &requirements);
    VkPhysicalDeviceMemoryProperties memory_types;
    vkGetPhysicalDeviceMemoryProperties(physical, &memory_types);
    const VkMemoryPropertyFlags host_memory =
        VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT;
    uint32_t type = 0;
    while (type < memory_types.memoryTypeCount &&
           (!(requirements.memoryTypeBits & (1u << type)) ||
            (memory_types.memoryTypes[type].propertyFlags & host_memory) != host_memory))
        type++;
    assert(type < memory_types.memoryTypeCount);
    VkDeviceMemory memory;
    VK(vkAllocateMemory(device, &(VkMemoryAllocateInfo){
        .sType = VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO,
        .allocationSize = requirements.size, .memoryTypeIndex = type
    }, NULL, &memory));
    VK(vkBindBufferMemory(device, buffer, memory, 0));

    VkDescriptorSetLayout descriptors;
    VK(vkCreateDescriptorSetLayout(device, &(VkDescriptorSetLayoutCreateInfo){
        .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_LAYOUT_CREATE_INFO, .bindingCount = 1,
        .pBindings = &(VkDescriptorSetLayoutBinding){
            .binding = 0, .descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER,
            .descriptorCount = 1, .stageFlags = VK_SHADER_STAGE_COMPUTE_BIT
        }
    }, NULL, &descriptors));
    VkDescriptorPool pool;
    VK(vkCreateDescriptorPool(device, &(VkDescriptorPoolCreateInfo){
        .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_POOL_CREATE_INFO, .maxSets = 1, .poolSizeCount = 1,
        .pPoolSizes = &(VkDescriptorPoolSize){VK_DESCRIPTOR_TYPE_STORAGE_BUFFER, 1}
    }, NULL, &pool));
    VkDescriptorSet set;
    VK(vkAllocateDescriptorSets(device, &(VkDescriptorSetAllocateInfo){
        .sType = VK_STRUCTURE_TYPE_DESCRIPTOR_SET_ALLOCATE_INFO, .descriptorPool = pool,
        .descriptorSetCount = 1, .pSetLayouts = &descriptors
    }, &set));
    vkUpdateDescriptorSets(device, 1, &(VkWriteDescriptorSet){
        .sType = VK_STRUCTURE_TYPE_WRITE_DESCRIPTOR_SET, .dstSet = set,
        .descriptorCount = 1, .descriptorType = VK_DESCRIPTOR_TYPE_STORAGE_BUFFER,
        .pBufferInfo = &(VkDescriptorBufferInfo){buffer, 0, size}
    }, 0, NULL);

    VkPipelineLayout layout;
    VK(vkCreatePipelineLayout(device, &(VkPipelineLayoutCreateInfo){
        .sType = VK_STRUCTURE_TYPE_PIPELINE_LAYOUT_CREATE_INFO, .setLayoutCount = 1,
        .pSetLayouts = &descriptors
    }, NULL, &layout));
    VkShaderModule shader;
    VK(vkCreateShaderModule(device, &(VkShaderModuleCreateInfo){
        .sType = VK_STRUCTURE_TYPE_SHADER_MODULE_CREATE_INFO, .codeSize = code_size, .pCode = code
    }, NULL, &shader));
    VkPipeline pipeline;
    VK(vkCreateComputePipelines(device, VK_NULL_HANDLE, 1, &(VkComputePipelineCreateInfo){
        .sType = VK_STRUCTURE_TYPE_COMPUTE_PIPELINE_CREATE_INFO, .layout = layout,
        .stage = {
            .sType = VK_STRUCTURE_TYPE_PIPELINE_SHADER_STAGE_CREATE_INFO,
            .stage = VK_SHADER_STAGE_COMPUTE_BIT, .module = shader, .pName = "main"
        }
    }, NULL, &pipeline));

    VkCommandPool commands;
    VK(vkCreateCommandPool(device, &(VkCommandPoolCreateInfo){
        .sType = VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO, .queueFamilyIndex = family
    }, NULL, &commands));
    VkCommandBuffer command;
    VK(vkAllocateCommandBuffers(device, &(VkCommandBufferAllocateInfo){
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO, .commandPool = commands,
        .commandBufferCount = 1, .level = VK_COMMAND_BUFFER_LEVEL_PRIMARY
    }, &command));
    VK(vkBeginCommandBuffer(command, &(VkCommandBufferBeginInfo){
        .sType = VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO
    }));
    vkCmdBindPipeline(command, VK_PIPELINE_BIND_POINT_COMPUTE, pipeline);
    vkCmdBindDescriptorSets(command, VK_PIPELINE_BIND_POINT_COMPUTE, layout, 0, 1, &set, 0, NULL);
    vkCmdDispatch(command, 1, 1, 1);
    vkCmdPipelineBarrier(command, VK_PIPELINE_STAGE_COMPUTE_SHADER_BIT,
        VK_PIPELINE_STAGE_HOST_BIT, 0, 1, &(VkMemoryBarrier){
            .sType = VK_STRUCTURE_TYPE_MEMORY_BARRIER,
            .srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT, .dstAccessMask = VK_ACCESS_HOST_READ_BIT
        }, 0, NULL, 0, NULL);
    VK(vkEndCommandBuffer(command));
    VK(vkQueueSubmit(queue, 1, &(VkSubmitInfo){
        .sType = VK_STRUCTURE_TYPE_SUBMIT_INFO, .commandBufferCount = 1, .pCommandBuffers = &command
    }, VK_NULL_HANDLE));
    VK(vkQueueWaitIdle(queue));

    void *data;
    VK(vkMapMemory(device, memory, 0, size, 0, &data));
    const uint32_t expected[] = {1, 4, 9, 16};
    assert(memcmp(data, expected, size) == 0);
    vkUnmapMemory(device, memory);
    vkDestroyCommandPool(device, commands, NULL);
    vkDestroyPipeline(device, pipeline, NULL);
    vkDestroyShaderModule(device, shader, NULL);
    vkDestroyPipelineLayout(device, layout, NULL);
    vkDestroyDescriptorPool(device, pool, NULL);
    vkDestroyDescriptorSetLayout(device, descriptors, NULL);
    vkDestroyBuffer(device, buffer, NULL);
    vkFreeMemory(device, memory, NULL);
    vkDestroyDevice(device, NULL);
    vkDestroyInstance(instance, NULL);
}
